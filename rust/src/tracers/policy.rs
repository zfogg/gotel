use crate::Query;

/// SQL adapter privacy and resource limits. Zero byte/work limits select Sequel's
/// defaults. No callback or parameter values are retained by the adapter.
#[derive(Clone, Copy, Debug, Default)]
pub struct SqlPolicy {
    /// Omit query text while retaining safe summaries and diagnostics.
    pub omit_query_text: bool,
    /// Include numeric source maps, still bounded by Gotel's transport limits.
    pub include_source_maps: bool,
    /// Include marker descriptors without names, types or values.
    pub include_bind_descriptors: bool,
    /// Maximum displayed query bytes; zero uses Sequel's default.
    pub max_query_text_bytes: usize,
    /// Maximum original SQL input bytes.
    pub max_input_bytes: usize,
    /// Maximum aggregate native output bytes.
    pub max_output_bytes: usize,
    /// Maximum native allocation bytes.
    pub max_memory_bytes: usize,
    /// Maximum native scanner/formatter work units.
    pub max_work_units: usize,
}
impl SqlPolicy {
    pub(super) fn query<'a>(&self, operation: &'static str, sql: &'a str) -> Query<'a> {
        Query::new(operation, sql).with_analysis(sequel::AnalysisOptions {
            format: true,
            omit_query_text: self.omit_query_text,
            omit_source_maps: !self.include_source_maps,
            omit_bind_descriptors: !self.include_bind_descriptors,
            max_query_text_bytes: self.max_query_text_bytes,
            max_input_bytes: self.max_input_bytes,
            max_output_bytes: self.max_output_bytes,
            max_memory_bytes: self.max_memory_bytes,
            max_work_units: self.max_work_units,
            ..Default::default()
        })
    }
}
