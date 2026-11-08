use super::tests::{assert_private, attributes, fixture};
use crate::Outcome;
use opentelemetry::{trace::SpanKind, Context, Value};

#[test]
fn duckdb_queries_native_types_errors_diagnostics_and_parentage() {
    let (app, exporter) = fixture();
    let db = super::duckdb::Connection::new(
        duckdb::Connection::open_in_memory().unwrap(),
        &app,
        "storage.duckdb",
        "analytics",
    )
    .unwrap();
    let parent = app
        .operation(&Context::new(), "Report", SpanKind::Internal)
        .unwrap();
    db.execute_batch(
        parent.context(),
        "Schema",
        "CREATE TABLE items (id BIGINT, value VARCHAR)",
    )
    .unwrap();
    assert_eq!(
        db.execute(
            parent.context(),
            "Insert",
            "INSERT INTO items VALUES (?, ?)",
            duckdb::params![42_i64, "PRIVATE_BIND"]
        )
        .unwrap(),
        1
    );
    let row: (i64, String) = db
        .query_row(
            parent.context(),
            "Read",
            "SELECT id, value FROM items WHERE value = ?",
            ["PRIVATE_BIND"],
            |row| Ok((row.get(0)?, row.get(1)?)),
        )
        .unwrap();
    assert_eq!(row, (42, "PRIVATE_BIND".into()));
    let rows: Vec<i64> = db
        .query_all(
            parent.context(),
            "Range",
            "SELECT i FROM range(?) AS t(i)",
            [3_i64],
            |row| row.get(0),
        )
        .unwrap();
    assert_eq!(rows, vec![0, 1, 2]);
    let empty: Vec<i64> = db
        .query_all(
            parent.context(),
            "Empty",
            "SELECT 1 WHERE false",
            [],
            |row| row.get(0),
        )
        .unwrap();
    assert!(empty.is_empty());
    let missing = db
        .query_row(
            parent.context(),
            "Missing",
            "SELECT 1 WHERE false",
            [],
            |row| row.get::<_, i64>(0),
        )
        .unwrap_err();
    assert!(matches!(missing, duckdb::Error::QueryReturnedNoRows));
    db.execute(
        parent.context(),
        "Malformed",
        "SELECT 'PRIVATE_UNCLOSED",
        [],
    )
    .unwrap_err();
    let callback = db
        .query_all(parent.context(), "Callback", "SELECT 1", [], |_| {
            Err::<(), _>(duckdb::Error::InvalidParameterName(
                "PRIVATE_CALLBACK".into(),
            ))
        })
        .unwrap_err();
    assert!(
        matches!(callback, duckdb::Error::InvalidParameterName(ref name) if name == "PRIVATE_CALLBACK")
    );
    parent.finish(Outcome::Success, None);
    app.force_flush().unwrap();
    let spans = exporter.get_finished_spans().unwrap();
    assert_private(&spans);
    let parent = spans.iter().find(|s| s.name == "adapter.Report").unwrap();
    for span in spans
        .iter()
        .filter(|s| s.name.starts_with("storage.duckdb."))
    {
        assert_eq!(span.parent_span_id, parent.span_context.span_id());
        assert_eq!(attributes(span, "db.system.name"), Value::from("duckdb"));
    }
    let read = spans
        .iter()
        .find(|s| s.name == "storage.duckdb.Read")
        .unwrap();
    assert!(attributes(read, "gotel.db.query.fingerprint")
        .as_str()
        .starts_with("sequel:v1:duckdb:"));
    assert_eq!(
        attributes(read, "db.query.diagnostics"),
        Value::Array(opentelemetry::Array::String(vec![]))
    );
    let malformed = spans
        .iter()
        .find(|s| s.name == "storage.duckdb.Malformed")
        .unwrap();
    assert!(!malformed
        .attributes
        .iter()
        .any(|a| a.key.as_str() == "db.query.text"));
    let Value::Array(opentelemetry::Array::String(diagnostics)) =
        attributes(malformed, "db.query.diagnostics")
    else {
        panic!("expected diagnostics");
    };
    assert!(diagnostics[0].as_str().contains("unterminated_string"));
    let native = db.into_inner();
    assert_eq!(
        native
            .query_row("SELECT count(*) FROM items", [], |row| row.get::<_, i64>(0))
            .unwrap(),
        1
    );
    app.shutdown().unwrap();
}

#[test]
fn duckdb_native_transactions_and_privacy_budgets() {
    let (app, exporter) = fixture();
    let mut native = duckdb::Connection::open_in_memory().unwrap();
    native
        .execute_batch("CREATE TABLE items (value VARCHAR)")
        .unwrap();
    let parent = Context::new();
    for commit in [false, true] {
        let tx = native.transaction().unwrap();
        let db = super::duckdb::Connection::new(&*tx, &app, "storage.duckdb", "analytics").unwrap();
        db.execute(
            &parent,
            "Insert",
            "INSERT INTO items VALUES (?)",
            ["PRIVATE_VALUE"],
        )
        .unwrap();
        drop(db);
        if commit {
            tx.commit().unwrap();
        } else {
            tx.rollback().unwrap();
        }
    }
    assert_eq!(
        native
            .query_row("SELECT count(*) FROM items", [], |row| row.get::<_, i64>(0))
            .unwrap(),
        1
    );
    for (operation, policy, reason) in [
        (
            "Hidden",
            super::SqlPolicy {
                omit_query_text: true,
                ..Default::default()
            },
            "policy",
        ),
        (
            "Budget",
            super::SqlPolicy {
                max_work_units: 1,
                ..Default::default()
            },
            "analysis_limit",
        ),
    ] {
        let db = super::duckdb::Connection::new(&native, &app, "storage.duckdb", "analytics")
            .unwrap()
            .with_policy(policy);
        let value: String = db
            .query_row(&parent, operation, "SELECT 'PRIVATE_LITERAL'", [], |row| {
                row.get(0)
            })
            .unwrap();
        assert_eq!(value, "PRIVATE_LITERAL");
        app.force_flush().unwrap();
        let spans = exporter.get_finished_spans().unwrap();
        let span = spans
            .iter()
            .find(|s| s.name == format!("storage.duckdb.{operation}"))
            .unwrap();
        assert!(!span
            .attributes
            .iter()
            .any(|a| a.key.as_str() == "db.query.text"));
        assert_eq!(
            attributes(span, "gotel.db.query.text_reason"),
            Value::from(reason)
        );
        assert_eq!(
            attributes(span, "operation.outcome"),
            Value::from("success")
        );
        assert_private(&spans);
    }
    app.shutdown().unwrap();
}
