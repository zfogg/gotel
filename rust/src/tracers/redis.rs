//! redis-rs 1.x connection adapter for `Commands`, `AsyncCommands` and pipelines.
//!
//! Only a fixed command-name vocabulary is exported. Keys, arguments, response
//! values, packed commands and Redis error messages never enter telemetry.
use super::observer::Observer;
use crate::{Application, NamingError, Operation, Outcome};
use opentelemetry::{
    trace::{FutureExt, SpanKind},
    Context, KeyValue,
};
use redis::{Cmd, Pipeline, RedisFuture, RedisResult, Value};

/// An owned or borrowed native connection, transparently implementing redis-rs
/// connection traits. Clone multiplexed connections before wrapping when sharing.
pub struct Connection<C> {
    inner: C,
    observer: Observer,
    parent: Option<Context>,
}
impl<C> Connection<C> {
    /// Wrap a connection/manager with application-owned providers and a safe prefix.
    pub fn new(inner: C, app: &Application, prefix: &str) -> Result<Self, NamingError> {
        Ok(Self {
            inner,
            observer: Observer::new(
                app,
                prefix,
                "db.client.operation.duration",
                "gotel.redis.command.count",
            )?,
            parent: None,
        })
    }
    /// Select explicit causal parentage instead of the context active on each call.
    pub fn with_parent(mut self, parent: Context) -> Self {
        self.parent = Some(parent);
        self
    }
    /// Return the original native connection without changing its state.
    pub fn into_inner(self) -> C {
        self.inner
    }
    fn start(&self, command: &'static str) -> Operation {
        let parent = self.parent.clone().unwrap_or_else(Context::current);
        let attrs = vec![
            KeyValue::new("db.system.name", "redis"),
            KeyValue::new("db.operation.name", command),
        ];
        self.observer.start(
            &parent,
            self.observer.names.name(command).expect("bounded command"),
            SpanKind::Client,
            attrs.clone(),
            attrs,
        )
    }
}
fn finish<T>(op: Operation, result: &RedisResult<T>) {
    op.finish(
        if result.is_ok() {
            Outcome::Success
        } else {
            Outcome::Error
        },
        result.as_ref().err().map(|_| "redis::RedisError"),
    );
}
fn command(bytes: &[u8]) -> &'static str {
    if bytes.len() > 32 {
        return "OTHER";
    }
    // A closed vocabulary prevents a custom command from exporting request data.
    match bytes.to_ascii_uppercase().as_slice() {
        b"GET" => "GET",
        b"SET" => "SET",
        b"DEL" => "DEL",
        b"MGET" => "MGET",
        b"MSET" => "MSET",
        b"EXISTS" => "EXISTS",
        b"EXPIRE" => "EXPIRE",
        b"TTL" => "TTL",
        b"PTTL" => "PTTL",
        b"INCR" => "INCR",
        b"INCRBY" => "INCRBY",
        b"DECR" => "DECR",
        b"HGET" => "HGET",
        b"HSET" => "HSET",
        b"HGETALL" => "HGETALL",
        b"HDEL" => "HDEL",
        b"HMGET" => "HMGET",
        b"LPUSH" => "LPUSH",
        b"RPUSH" => "RPUSH",
        b"LPOP" => "LPOP",
        b"RPOP" => "RPOP",
        b"LRANGE" => "LRANGE",
        b"BLPOP" => "BLPOP",
        b"BRPOP" => "BRPOP",
        b"SADD" => "SADD",
        b"SREM" => "SREM",
        b"SMEMBERS" => "SMEMBERS",
        b"SISMEMBER" => "SISMEMBER",
        b"ZADD" => "ZADD",
        b"ZRANGE" => "ZRANGE",
        b"ZREM" => "ZREM",
        b"SCAN" => "SCAN",
        b"HSCAN" => "HSCAN",
        b"SSCAN" => "SSCAN",
        b"ZSCAN" => "ZSCAN",
        b"PING" => "PING",
        b"AUTH" => "AUTH",
        b"SELECT" => "SELECT",
        b"EVAL" => "EVAL",
        b"EVALSHA" => "EVALSHA",
        b"SCRIPT" => "SCRIPT",
        b"MULTI" => "MULTI",
        b"EXEC" => "EXEC",
        b"DISCARD" => "DISCARD",
        b"WATCH" => "WATCH",
        b"UNWATCH" => "UNWATCH",
        b"PUBLISH" => "PUBLISH",
        b"SUBSCRIBE" => "SUBSCRIBE",
        b"XADD" => "XADD",
        b"XREAD" => "XREAD",
        b"XREADGROUP" => "XREADGROUP",
        b"XACK" => "XACK",
        b"XGROUP" => "XGROUP",
        b"UNLINK" => "UNLINK",
        b"GETDEL" => "GETDEL",
        b"SETEX" => "SETEX",
        _ => "OTHER",
    }
}
fn cmd_name(cmd: &Cmd) -> &'static str {
    match cmd.args_iter().next() {
        Some(redis::Arg::Simple(bytes)) => command(bytes),
        _ => "OTHER",
    }
}
fn packed_name(bytes: &[u8]) -> &'static str {
    // Parse only the first RESP bulk argument, with strictly bounded scanning.
    let prefix = &bytes[..bytes.len().min(80)];
    let Some(first) = prefix.windows(2).position(|b| b == b"\r\n") else {
        return "OTHER";
    };
    let Some(rest) = prefix.get(first + 2..).and_then(|p| p.strip_prefix(b"$")) else {
        return "OTHER";
    };
    let Some(end) = rest.windows(2).position(|b| b == b"\r\n") else {
        return "OTHER";
    };
    let Some(length) = std::str::from_utf8(&rest[..end])
        .ok()
        .and_then(|s| s.parse::<usize>().ok())
        .filter(|n| *n <= 32)
    else {
        return "OTHER";
    };
    rest.get(end + 2..end + 2 + length)
        .map(command)
        .unwrap_or("OTHER")
}
impl<C: redis::ConnectionLike> redis::ConnectionLike for Connection<C> {
    fn req_command(&mut self, cmd: &Cmd) -> RedisResult<Value> {
        let op = self.start(cmd_name(cmd));
        let _guard = op.context().clone().attach();
        let result = self.inner.req_command(cmd);
        finish(op, &result);
        result
    }
    fn req_packed_command(&mut self, bytes: &[u8]) -> RedisResult<Value> {
        let op = self.start(packed_name(bytes));
        let _guard = op.context().clone().attach();
        let result = self.inner.req_packed_command(bytes);
        finish(op, &result);
        result
    }
    fn req_packed_commands(
        &mut self,
        bytes: &[u8],
        offset: usize,
        count: usize,
    ) -> RedisResult<Vec<Value>> {
        let op = self.start("PIPELINE");
        let _guard = op.context().clone().attach();
        let result = self.inner.req_packed_commands(bytes, offset, count);
        finish(op, &result);
        result
    }
    fn get_db(&self) -> i64 {
        self.inner.get_db()
    }
    fn supports_pipelining(&self) -> bool {
        self.inner.supports_pipelining()
    }
    fn check_connection(&mut self) -> bool {
        self.inner.check_connection()
    }
    fn is_open(&self) -> bool {
        self.inner.is_open()
    }
}
impl<C: redis::aio::ConnectionLike + Send> redis::aio::ConnectionLike for Connection<C> {
    fn req_packed_command<'a>(&'a mut self, cmd: &'a Cmd) -> RedisFuture<'a, Value> {
        Box::pin(async move {
            let op = self.start(cmd_name(cmd));
            let result = self
                .inner
                .req_packed_command(cmd)
                .with_context(op.context().clone())
                .await;
            finish(op, &result);
            result
        })
    }
    fn req_packed_commands<'a>(
        &'a mut self,
        pipeline: &'a Pipeline,
        offset: usize,
        count: usize,
    ) -> RedisFuture<'a, Vec<Value>> {
        Box::pin(async move {
            let op = self.start("PIPELINE");
            let result = self
                .inner
                .req_packed_commands(pipeline, offset, count)
                .with_context(op.context().clone())
                .await;
            finish(op, &result);
            result
        })
    }
    fn get_db(&self) -> i64 {
        self.inner.get_db()
    }
}
