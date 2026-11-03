use crate::{operation::QueryMetrics, Namer, Operation, Outcome};
use opentelemetry::{
    metrics::{Counter, Histogram, Meter},
    trace::{FutureExt, SpanKind, TraceContextExt, Tracer},
    Array, Context, KeyValue, Value,
};
use opentelemetry_sdk::trace::SdkTracer;
use sequel::{Analysis, AnalysisOptions, Dialect, SourceMap};
use serde_json::{json, Value as Json};
use std::future::Future;

/// Maximum JSON bytes per map/bind attribute and for all diagnostic strings.
pub const MAX_ANALYSIS_ATTRIBUTE_BYTES: usize = 16 * 1024;
const MAX_ITEMS: usize = MAX_ANALYSIS_ATTRIBUTE_BYTES / 96;

/// Borrowed query template and per-call policy. Argument values are never accepted.
#[derive(Clone, Copy)]
pub struct Query<'a> {
    /// Source-owned operation identifier, not SQL or request data.
    pub operation: &'static str,
    /// Original SQL template, borrowed only during synchronous analysis.
    pub statement: &'a str,
    /// Optional supplied arity, without values.
    pub supplied_slots: Option<usize>,
    /// Native budgets and privacy policy. The tracer selects the actual dialect.
    pub analysis: AnalysisOptions<'a>,
}
impl<'a> Query<'a> {
    /// Default safe formatted text, omitting optional maps/bind arrays.
    pub fn new(operation: &'static str, statement: &'a str) -> Self {
        Self {
            operation,
            statement,
            supplied_slots: None,
            analysis: AnalysisOptions {
                format: true,
                omit_source_maps: true,
                omit_bind_descriptors: true,
                ..Default::default()
            },
        }
    }
    /// Record only argument count; driver values remain inaccessible to Gotel.
    pub fn with_bind_count(mut self, count: usize) -> Self {
        self.supplied_slots = Some(count);
        self
    }
    /// Select explicit budgets, maps, descriptors or collection policy.
    pub fn with_analysis(mut self, analysis: AnalysisOptions<'a>) -> Self {
        self.analysis = analysis;
        self
    }
}

/// Readable source-owned name and bounded safe transport attributes.
#[derive(Debug, Clone)]
pub struct QueryMetadata {
    /// Checked source-owned database operation name.
    pub name: String,
    /// Standard database fields plus canonical diagnostic and provenance metadata.
    pub attributes: Vec<KeyValue>,
}

/// Database tracer tied to application-owned providers and a source-owned name
/// namespace. No driver, pool, connection, SQL text or argument values are retained.
pub struct SqlTracer {
    tracer: SdkTracer,
    namer: Namer,
    dialect: Dialect,
    namespace: String,
    server: Option<(String, u16)>,
    duration: Histogram<f64>,
    count: Counter<u64>,
}
impl SqlTracer {
    pub(crate) fn new(
        tracer: SdkTracer,
        meter: Meter,
        namer: Namer,
        dialect: Dialect,
        namespace: String,
    ) -> Self {
        Self {
            tracer,
            namer,
            dialect,
            namespace,
            server: None,
            duration: meter
                .f64_histogram("db.client.operation.duration")
                .with_unit("s")
                .build(),
            count: meter.u64_counter("gotel.db.query.count").build(),
        }
    }
    /// Set actual connection identity for cross-trace query grouping. Supply the
    /// hostname only, without credentials, a connection string or URL query.
    pub fn with_server(mut self, address: impl Into<String>, port: u16) -> Self {
        self.server = Some((address.into(), port));
        self
    }
    fn system(&self) -> &'static str {
        match self.dialect {
            Dialect::Generic => "other_sql",
            Dialect::Sqlite => "sqlite",
            Dialect::PostgreSql => "postgresql",
            Dialect::DuckDb => "duckdb",
            Dialect::MySql => "mysql",
            Dialect::ClickHouse => "clickhouse",
        }
    }
    /// Analyze once without reading values. The configured driver dialect takes
    /// precedence over the options' dialect. Invalid operation labels use `SQL`
    /// without echoing rejected input. Analysis failures never require a DB call
    /// to fail and always export an explicit absence reason.
    pub fn inspect(&self, query: Query<'_>) -> QueryMetadata {
        let Query {
            operation,
            statement: sql,
            supplied_slots,
            analysis: options,
        } = query;
        let name = self
            .namer
            .name(operation)
            .unwrap_or_else(|_| self.namer.name("SQL").expect("validated namespace"));
        let policy = AnalysisOptions {
            dialect: self.dialect,
            ..options
        };
        let mut attributes = vec![
            KeyValue::new("db.system.name", self.system()),
            KeyValue::new("db.namespace", self.namespace.clone()),
            KeyValue::new("gotel.db.query.original_length", sql.len() as i64),
            KeyValue::new("gotel.db.query.diagnostic_schema_version", 1i64),
        ];
        if let Some((address, port)) = &self.server {
            attributes.push(KeyValue::new("server.address", address.clone()));
            attributes.push(KeyValue::new("server.port", i64::from(*port)));
        }
        if let Some(count) = supplied_slots {
            attributes.push(KeyValue::new(
                "gotel.db.query.bind_count",
                i64::try_from(count).unwrap_or(i64::MAX),
            ));
        }
        match sequel::analyze(sql, &policy) {
            Ok(result) => append_analysis(&mut attributes, &result, supplied_slots, &policy),
            Err(_) => attributes.extend([
                KeyValue::new("gotel.db.query.analysis_status", "error"),
                KeyValue::new("gotel.db.query.text_reason", "analysis_error"),
                KeyValue::new("db.query.summary", "SQL"),
                diagnostic_array(Vec::new()),
            ]),
        }
        QueryMetadata { name, attributes }
    }
    /// Start an explicitly parented query span. No query is executed. Call finish
    /// with an explicit result, or use the sync/async helpers to preserve results.
    pub fn start(&self, parent: &Context, query: Query<'_>) -> Operation {
        let metadata = self.inspect(query);
        let operation = metadata
            .attributes
            .iter()
            .find(|a| a.key.as_str() == "db.operation.name")
            .map(|a| a.value.clone())
            .unwrap_or_else(|| "SQL".into());
        let labels = vec![
            KeyValue::new("db.system.name", self.system()),
            KeyValue::new("db.namespace", self.namespace.clone()),
            KeyValue::new("db.operation.name", operation),
        ];
        let span = self
            .tracer
            .span_builder(metadata.name)
            .with_kind(SpanKind::Client)
            .with_attributes(metadata.attributes)
            .start_with_context(&self.tracer, parent);
        Operation::new(
            parent.with_span(span),
            Some(QueryMetrics {
                duration: self.duration.clone(),
                count: self.count.clone(),
            }),
            labels,
        )
    }
    /// Execute a synchronous call exactly once; return its original result/error.
    /// No parameters or exception messages are inspected or recorded.
    pub fn query<T, E>(
        &self,
        parent: &Context,
        query: Query<'_>,
        driver: impl FnOnce() -> Result<T, E>,
    ) -> Result<T, E> {
        self.query_with(parent, query, driver, |_| Outcome::Error)
    }
    /// Synchronous execution with explicit cancellation/deferred/error classification.
    pub fn query_with<T, E>(
        &self,
        parent: &Context,
        query: Query<'_>,
        driver: impl FnOnce() -> Result<T, E>,
        classify: impl FnOnce(&E) -> Outcome,
    ) -> Result<T, E> {
        let op = self.start(parent, query);
        let _guard = op.context().clone().attach();
        let result = driver();
        finish_result(op, &result, classify);
        result
    }
    /// Analyze and construct the driver future now, then attach context on every
    /// poll, never across await. The returned future owns its span and holds no
    /// policy/cancellation-callback borrow. Dropping it records cancellation.
    pub fn query_async<T, E, Fut: Future<Output = Result<T, E>>>(
        &self,
        parent: &Context,
        query: Query<'_>,
        driver: impl FnOnce() -> Fut,
    ) -> impl Future<Output = Result<T, E>> {
        self.query_async_with(parent, query, driver, |_| Outcome::Error)
    }
    /// Async execution with explicit error classification and unchanged results.
    pub fn query_async_with<T, E, Fut: Future<Output = Result<T, E>>>(
        &self,
        parent: &Context,
        query: Query<'_>,
        driver: impl FnOnce() -> Fut,
        classify: impl FnOnce(&E) -> Outcome,
    ) -> impl Future<Output = Result<T, E>> {
        let op = self.start(parent, query);
        let context = op.context().clone();
        let future = {
            let _guard = context.clone().attach();
            driver()
        };
        async move {
            let result = future.with_context(context).await;
            finish_result(op, &result, classify);
            result
        }
    }
}
fn finish_result<T, E>(op: Operation, result: &Result<T, E>, classify: impl FnOnce(&E) -> Outcome) {
    let outcome = match result {
        Ok(_) => Outcome::Success,
        Err(e) => classify(e),
    };
    op.finish(
        outcome,
        (outcome == Outcome::Error).then_some(std::any::type_name::<E>()),
    );
}
fn diagnostic_array(items: Vec<String>) -> KeyValue {
    KeyValue::new(
        "db.query.diagnostics",
        Value::Array(Array::String(items.into_iter().map(Into::into).collect())),
    )
}
fn bounded(value: Json) -> Option<String> {
    let text = value.to_string();
    (text.len() <= MAX_ANALYSIS_ATTRIBUTE_BYTES).then_some(text)
}
fn map_json(map: &SourceMap) -> Json {
    json!({"from_length":map.from_length,"to_length":map.to_length,"segments":map.segments.iter().map(|s|json!({
        "from_start":s.from_start,"from_end":s.from_end,"to_start":s.to_start,"to_end":s.to_end,
        "kind":match s.kind {0=>"copy",1=>"replace",2=>"delete",3=>"insert",_=>"unknown"}
    })).collect::<Vec<_>>()})
}
fn append_analysis(
    attrs: &mut Vec<KeyValue>,
    a: &Analysis,
    supplied: Option<usize>,
    policy: &AnalysisOptions<'_>,
) {
    attrs.push(KeyValue::new(
        "gotel.db.query.analysis_status",
        a.status.clone(),
    ));
    attrs.push(KeyValue::new("db.query.summary", a.summary.clone()));
    if !a.operation.is_empty() {
        attrs.push(KeyValue::new("db.operation.name", a.operation.clone()));
    }
    if !a.fingerprint.is_empty() {
        attrs.push(KeyValue::new(
            "gotel.db.query.fingerprint",
            a.fingerprint.clone(),
        ));
    }
    if !a.omission_reason.is_empty() {
        attrs.push(KeyValue::new(
            "gotel.db.query.text_reason",
            a.omission_reason.clone(),
        ));
    }
    let formatted = !a.formatted.is_empty()
        && (policy.max_query_text_bytes == 0 || a.formatted.len() <= policy.max_query_text_bytes);
    let text = if formatted { &a.formatted } else { &a.text };
    if !text.is_empty() {
        attrs.push(KeyValue::new("db.query.text", text.clone()));
    }
    if a.truncated {
        attrs.push(KeyValue::new("gotel.db.query.truncated", true));
    }
    let mut diagnostics = Vec::new();
    let mut bytes = 0;
    for d in a.diagnostics.iter().take(64) {
        let text=json!({"code":d.code_name().unwrap_or("unknown"),"severity":d.severity_name(),"offset":d.range.start,"end_offset":d.range.end,
            "line":d.line,"column":d.column,"end_line":d.end_line,"end_column":d.end_column,"incomplete":d.incomplete,"statement_complete":d.statement_complete,
            "message":d.message,"explanation":d.explanation,"hint":d.hint}).to_string();
        if bytes + text.len() + 2 > MAX_ANALYSIS_ATTRIBUTE_BYTES {
            break;
        }
        bytes += text.len() + 2;
        diagnostics.push(text);
    }
    if diagnostics.len() < a.diagnostics.len() {
        attrs.push(KeyValue::new("gotel.db.query.diagnostics_omitted", true));
    }
    attrs.push(diagnostic_array(diagnostics));
    if a.status != "ok" {
        return;
    }
    attrs.push(KeyValue::new(
        "gotel.db.query.statement_count",
        a.statements.len() as i64,
    ));
    if !policy.omit_source_maps && !text.is_empty() {
        let encoded = if a.original_to_sanitized.segments.len()
            + a.sanitized_to_formatted.segments.len()
            <= MAX_ITEMS
        {
            bounded(
                json!({"schema_version":a.schema_version,"original_to_sanitized":map_json(&a.original_to_sanitized),
                "sanitized_to_formatted":map_json(&a.sanitized_to_formatted),"display":if formatted {"formatted"} else {"sanitized"}}),
            )
        } else {
            None
        };
        if let Some(json) = encoded {
            attrs.push(KeyValue::new("gotel.db.query.source_map", json));
        } else {
            attrs.push(KeyValue::new("gotel.db.query.source_map_omitted", true));
        }
    }
    if !policy.omit_bind_descriptors {
        let encoded = if a.binds.len() <= MAX_ITEMS {
            bounded(json!(a.binds.iter().map(|b|json!({
            "ordinal":b.ordinal,"slot":b.slot,"statement_index":b.statement_index,"offset":b.offset,"end_offset":b.end_offset,
            "style":match b.style {0=>"positional",1=>"numbered",2=>"named",3=>"typed",_=>"unknown"}
        })).collect::<Vec<_>>()))
        } else {
            None
        };
        if let Some(json) = encoded {
            attrs.push(KeyValue::new("gotel.db.query.binds", json));
        } else {
            attrs.push(KeyValue::new("gotel.db.query.binds_omitted", true));
        }
        if let Some(count) = supplied.filter(|_| a.statements.len() == 1) {
            attrs.push(KeyValue::new(
                "gotel.db.query.bind_arity_valid",
                a.check_binds(0, count).is_ok(),
            ));
        }
    }
}
