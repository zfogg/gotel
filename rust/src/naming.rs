use std::fmt;

/// An invalid configuration or source-owned operation identifier.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NamingError;
impl fmt::Display for NamingError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("operation names must be nonempty source-owned identifiers")
    }
}
impl std::error::Error for NamingError {}

/// Validate a name without echoing an invalid input into an error.
pub fn validate_name(value: &str) -> Result<(), NamingError> {
    let mut chars = value.bytes();
    if value.len() > 255
        || !chars
            .next()
            .is_some_and(|b| b.is_ascii_alphabetic() || b == b'_')
        || !chars.all(|b| b.is_ascii_alphanumeric() || matches!(b, b'_' | b'.' | b'-'))
    {
        return Err(NamingError);
    }
    Ok(())
}

/// A source-owned database span namespace, for example `storage.postgres`.
#[derive(Debug, Clone)]
pub struct Namer {
    prefix: String,
}
impl Namer {
    /// Validate the application-owned prefix once.
    pub fn new(prefix: impl Into<String>) -> Result<Self, NamingError> {
        let prefix = prefix.into();
        validate_name(&prefix)?;
        Ok(Self { prefix })
    }
    /// Combine a static operation name with this namespace.
    pub fn name(&self, operation: &'static str) -> Result<String, NamingError> {
        validate_name(operation)?;
        if operation.starts_with(&format!("{}.", self.prefix)) {
            return Ok(operation.into());
        }
        let result = format!("{}.{}", self.prefix, operation);
        validate_name(&result)?;
        Ok(result)
    }
}

/// A safe, low-cardinality HTTP span name. Server targets must be route templates;
/// client URLs lose userinfo, query and fragment. Common ID segments are replaced.
pub fn http_name(
    prefix: &str,
    server: bool,
    method: &str,
    target: &str,
) -> Result<String, NamingError> {
    validate_name(prefix)?;
    if target.len() > 4096 || (server && !target.starts_with('/')) {
        return Err(NamingError);
    }
    let method = method.to_ascii_uppercase();
    let method = match method.as_str() {
        "GET" | "POST" | "PUT" | "PATCH" | "DELETE" | "HEAD" | "OPTIONS" | "CONNECT" | "TRACE" => {
            method
        }
        _ => "OTHER".into(),
    };
    let target = if server {
        // The owner supplies a template, never a request URL with values.
        target.split(['?', '#']).next().unwrap_or("/").to_owned()
    } else {
        let parsed = url::Url::parse(target).map_err(|_| NamingError)?;
        if !matches!(parsed.scheme(), "http" | "https") {
            return Err(NamingError);
        }
        parsed
            .path()
            .split('/')
            .map(|segment| {
                if segment.parse::<u64>().is_ok()
                    || (segment.len() >= 16
                        && segment.bytes().all(|b| b.is_ascii_hexdigit() || b == b'-'))
                {
                    ":id"
                } else {
                    segment
                }
            })
            .collect::<Vec<_>>()
            .join("/")
    };
    Ok(format!("{prefix}.{method} {target}"))
}
