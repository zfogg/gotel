//! Exercise the real HTTP/protobuf exporters against a local collector.
use super::*;
use opentelemetry::{
    logs::{LogRecord, Logger, LoggerProvider},
    metrics::MeterProvider,
    Context,
};
use prost::Message;
use std::{
    io::{Read, Write},
    net::TcpListener,
    sync::{
        atomic::{AtomicBool, Ordering},
        Arc, Mutex,
    },
    time::{Duration, Instant},
};

#[test]
fn exports_all_signals_to_prefixed_endpoint() {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let address = listener.local_addr().unwrap();
    listener.set_nonblocking(true).unwrap();
    let done = Arc::new(AtomicBool::new(false));
    let received = Arc::new(Mutex::new(Vec::new()));
    let stopped = done.clone();
    let requests = received.clone();
    let collector = std::thread::spawn(move || {
        let deadline = Instant::now() + Duration::from_secs(30);
        while !stopped.load(Ordering::Relaxed) && Instant::now() < deadline {
            let (mut stream, _) = match listener.accept() {
                Ok(connection) => connection,
                Err(e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                    std::thread::sleep(Duration::from_millis(5));
                    continue;
                }
                Err(e) => panic!("collector accept: {e}"),
            };
            stream.set_nonblocking(false).unwrap();
            stream
                .set_read_timeout(Some(Duration::from_secs(5)))
                .unwrap();
            let mut header = Vec::new();
            while !header.ends_with(b"\r\n\r\n") {
                assert!(header.len() < 65536);
                let mut byte = [0];
                stream.read_exact(&mut byte).unwrap();
                header.push(byte[0]);
            }
            let header = String::from_utf8(header).unwrap();
            let path = header
                .lines()
                .next()
                .unwrap()
                .split_whitespace()
                .nth(1)
                .unwrap()
                .to_owned();
            let length: usize = header
                .lines()
                .find_map(|line| {
                    let (name, value) = line.split_once(':')?;
                    name.eq_ignore_ascii_case("content-length")
                        .then(|| value.trim().parse().unwrap())
                })
                .unwrap();
            assert!(length <= 8 * 1024 * 1024);
            let mut body = vec![0; length];
            stream.read_exact(&mut body).unwrap();
            requests.lock().unwrap().push((path, body));
            stream.write_all(b"HTTP/1.1 200 OK\r\nContent-Type: application/x-protobuf\r\nContent-Length: 0\r\nConnection: close\r\n\r\n").unwrap();
        }
    });
    let mut config = ApplicationConfig::new("rust-export-test");
    config.endpoint = Some(format!("http://{address}/collector"));
    config.bypass_proxy = true;
    let app = Application::otlp(config).unwrap();
    let db = app
        .database("storage.sqlite", sequel::Dialect::Sqlite, "test")
        .unwrap();
    db.query(
        &Context::new(),
        Query::new("Read", "SELECT 'PRIVATE_LITERAL'"),
        || Ok::<_, ()>(()),
    )
    .unwrap();
    app.meter_provider()
        .meter("test")
        .u64_counter("test.counter")
        .build()
        .add(1, &[]);
    let logger = app.logger_provider().logger("test");
    let mut record = logger.create_log_record();
    record.set_body("safe structured log".into());
    logger.emit(record);
    app.force_flush().unwrap();
    app.shutdown().unwrap();
    done.store(true, Ordering::Relaxed);
    collector.join().unwrap();
    let requests = received.lock().unwrap();
    let body = |signal: &str| {
        requests
            .iter()
            .find(|(path, _)| path == &format!("/collector/v1/{signal}"))
            .unwrap()
            .1
            .as_slice()
    };
    use opentelemetry_proto::tonic::collector::{
        logs::v1::ExportLogsServiceRequest, metrics::v1::ExportMetricsServiceRequest,
        trace::v1::ExportTraceServiceRequest,
    };
    let traces = ExportTraceServiceRequest::decode(body("traces")).unwrap();
    let spans = &traces.resource_spans[0].scope_spans[0].spans;
    assert_eq!(spans[0].name, "storage.sqlite.Read");
    assert!(spans[0].attributes.iter().any(|a| a.key == "db.query.text"));
    assert!(spans[0]
        .attributes
        .iter()
        .any(|a| a.key == "db.query.diagnostics"));
    assert!(!format!("{traces:?}").contains("PRIVATE_LITERAL"));
    let metrics = ExportMetricsServiceRequest::decode(body("metrics")).unwrap();
    assert!(metrics
        .resource_metrics
        .iter()
        .flat_map(|r| &r.scope_metrics)
        .flat_map(|s| &s.metrics)
        .any(|m| m.name == "gotel.db.query.count"));
    let logs = ExportLogsServiceRequest::decode(body("logs")).unwrap();
    assert_eq!(logs.resource_logs[0].scope_logs[0].log_records.len(), 1);
}
