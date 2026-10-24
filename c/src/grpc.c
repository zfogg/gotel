/* Native unary OTLP/gRPC delivery behind the pinned exporter's request API.
 * HTTP outcome codes below are internal delivery classes, not wire statuses. */
#include "http1_client.h"
#include "protobuf_decode.h"
#include "tls.h"
#include <openssl/evp.h>
#define NGHTTP2_NO_SSIZE_T
#include <nghttp2/nghttp2.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define RESPONSE_LIMIT (4u * 1024u * 1024u)
struct otlp_http_request {
  gotel_http1_request_t *http1;
  otlp_socket_t *socket;
  gotel_tls *tls;
  nghttp2_session *session;
  otlp_http_req_state_t state;
  uint8_t *send, *response;
  size_t send_len, send_at, response_len, response_cap;
  const uint8_t *wire;
  size_t wire_len, wire_at;
  uint64_t started, progress;
  uint32_t connect_ms, read_ms;
  int32_t stream;
  int status, grpc_status, content_type, events;
  size_t header_bytes;
  uint32_t retry_ms;
  bool closed, invalid, connected, retry_info, too_large;
};

static int equals(const uint8_t *data, size_t len, const char *text) {
  return len == strlen(text) && !memcmp(data, text, len);
}
static int number(const uint8_t *data, size_t len, int maximum) {
  int n = 0;
  if (!len)
    return -1;
  for (size_t i = 0; i < len; ++i) {
    if (data[i] < '0' || data[i] > '9' || n > (maximum - (data[i] - '0')) / 10)
      return -1;
    n = n * 10 + data[i] - '0';
  }
  return n;
}
/* Decode only the bounded google.rpc.Status -> Any -> RetryInfo -> Duration
 * path. All other protobuf fields are skipped by the upstream checked reader. */
static bool message_field(const uint8_t *bytes, size_t len, uint32_t wanted, const uint8_t **out, size_t *size) {
  struct otlp_pb_reader reader;
  otlp_pb_reader_init(&reader, bytes, len);
  uint32_t field;
  int wire;
  while (reader.pos < reader.len) {
    if (!otlp_pb_read_key(&reader, &field, &wire))
      return false;
    if (field == wanted && wire == 2)
      return otlp_pb_read_len(&reader, out, size);
    if (!otlp_pb_skip(&reader, wire))
      return false;
  }
  return false;
}
static void retry_details(otlp_http_request_t *r, const uint8_t *value, size_t len) {
  if (!len || len > 8192)
    return;
  unsigned char encoded[8197], decoded[6147];
  memcpy(encoded, value, len);
  size_t padded = len;
  while (padded % 4)
    encoded[padded++] = '=';
  int n = EVP_DecodeBlock(decoded, encoded, (int)padded);
  if (n < 0)
    return;
  while (padded && encoded[--padded] == '=')
    --n;
  if (n <= 0)
    return;
  struct otlp_pb_reader status;
  otlp_pb_reader_init(&status, decoded, (size_t)n);
  uint32_t field;
  int wire;
  while (status.pos < status.len) {
    if (!otlp_pb_read_key(&status, &field, &wire))
      return;
    if (field != 3 || wire != 2) {
      if (!otlp_pb_skip(&status, wire))
        return;
      continue;
    }
    const uint8_t *any, *type, *info, *duration;
    size_t anylen, typelen, infolen, durationlen;
    if (!otlp_pb_read_len(&status, &any, &anylen))
      return;
    if (!message_field(any, anylen, 1, &type, &typelen) ||
        !equals(type, typelen, "type.googleapis.com/google.rpc.RetryInfo") ||
        !message_field(any, anylen, 2, &info, &infolen) || !message_field(info, infolen, 1, &duration, &durationlen))
      continue;
    struct otlp_pb_reader delay;
    otlp_pb_reader_init(&delay, duration, durationlen);
    uint64_t seconds = 0, nanos = 0, number_value;
    while (delay.pos < delay.len) {
      if (!otlp_pb_read_key(&delay, &field, &wire))
        return;
      if ((field == 1 || field == 2) && wire == 0) {
        if (!otlp_pb_read_varint(&delay, &number_value))
          return;
        if (field == 1)
          seconds = number_value;
        else
          nanos = number_value;
      } else if (!otlp_pb_skip(&delay, wire))
        return;
    }
    if (seconds > INT64_MAX || nanos > 999999999)
      return;
    r->retry_info = true;
    uint64_t ms = seconds > UINT32_MAX / 1000 ? UINT32_MAX : seconds * 1000 + (nanos + 999999) / 1000000;
    r->retry_ms = ms > UINT32_MAX ? UINT32_MAX : (uint32_t)ms;
    return;
  }
}
static int header(nghttp2_session *session, const nghttp2_frame *frame, const uint8_t *name, size_t namelen,
                  const uint8_t *value, size_t valuelen, uint8_t flags, void *context) {
  (void)session;
  (void)flags;
  otlp_http_request_t *r = context;
  if (frame->hd.stream_id != r->stream)
    return 0;
  if (namelen > 16384 || valuelen > 16384 - namelen || r->header_bytes > 16384 - namelen - valuelen)
    return NGHTTP2_ERR_CALLBACK_FAILURE;
  r->header_bytes += namelen + valuelen;
  if (equals(name, namelen, ":status")) {
    if (r->status)
      r->invalid = true;
    r->status = number(value, valuelen, 999);
  } else if (equals(name, namelen, "grpc-status")) {
    if (r->grpc_status != -1 || !(frame->hd.flags & NGHTTP2_FLAG_END_STREAM))
      r->invalid = true;
    r->grpc_status = number(value, valuelen, 16);
    if (r->grpc_status < 0)
      r->invalid = true;
  } else if (equals(name, namelen, "content-type")) {
    r->content_type = equals(value, valuelen, "application/grpc") || equals(value, valuelen, "application/grpc+proto");
  } else if (equals(name, namelen, "grpc-encoding") && !equals(value, valuelen, "identity"))
    r->invalid = true;
  else if (equals(name, namelen, "grpc-status-details-bin"))
    retry_details(r, value, valuelen);
  return 0;
}
static int data(nghttp2_session *session, uint8_t flags, int32_t stream, const uint8_t *bytes, size_t len,
                void *context) {
  (void)session;
  (void)flags;
  otlp_http_request_t *r = context;
  if (stream != r->stream)
    return 0;
  if (len > RESPONSE_LIMIT - r->response_len) {
    r->too_large = true;
    return NGHTTP2_ERR_CALLBACK_FAILURE;
  }
  size_t need = r->response_len + len;
  if (need > r->response_cap) {
    size_t cap = need < 4096 ? 4096 : need * 2;
    if (cap > RESPONSE_LIMIT)
      cap = RESPONSE_LIMIT;
    uint8_t *buffer = realloc(r->response, cap);
    if (!buffer)
      return NGHTTP2_ERR_CALLBACK_FAILURE;
    r->response = buffer;
    r->response_cap = cap;
  }
  if (len)
    memcpy(r->response + r->response_len, bytes, len);
  r->response_len = need;
  if (need >= 5) {
    uint32_t declared = ((uint32_t)r->response[1] << 24) | ((uint32_t)r->response[2] << 16) |
                        ((uint32_t)r->response[3] << 8) | r->response[4];
    if (declared > RESPONSE_LIMIT - 5) {
      r->too_large = true;
      return NGHTTP2_ERR_CALLBACK_FAILURE;
    }
  }
  return 0;
}
static int close_stream(nghttp2_session *session, int32_t stream, uint32_t error, void *context) {
  (void)session;
  otlp_http_request_t *r = context;
  if (stream == r->stream) {
    r->closed = true;
    r->invalid |= error != NGHTTP2_NO_ERROR;
  }
  return 0;
}
static nghttp2_ssize read_body(nghttp2_session *session, int32_t stream, uint8_t *buffer, size_t len, uint32_t *flags,
                               nghttp2_data_source *source, void *context) {
  (void)session;
  (void)stream;
  (void)source;
  otlp_http_request_t *r = context;
  size_t n = r->send_len - r->send_at;
  if (n > len)
    n = len;
  memcpy(buffer, r->send + r->send_at, n);
  r->send_at += n;
  if (r->send_at == r->send_len)
    *flags |= NGHTTP2_DATA_FLAG_EOF;
  return (nghttp2_ssize)n;
}
static nghttp2_nv nv(const char *name, const char *value) {
  nghttp2_nv result = {(uint8_t *)name, (uint8_t *)value, strlen(name), strlen(value), NGHTTP2_NV_FLAG_NONE};
  return result;
}
static bool metadata_valid(const char *name, const char *value) {
  if (!name || !*name || !value || name[0] == ':' || !strncmp(name, "grpc-", 5))
    return false;
  for (const unsigned char *p = (const unsigned char *)name; *p; ++p)
    if (!((*p >= 'a' && *p <= 'z') || (*p >= '0' && *p <= '9') || *p == '-' || *p == '_' || *p == '.'))
      return false;
  const char *reserved[] = {"content-type",   "te",         "host",    "connection", "transfer-encoding",
                            "content-length", "keep-alive", "upgrade", "user-agent"};
  for (size_t i = 0; i < sizeof(reserved) / sizeof(*reserved); ++i)
    if (!strcmp(name, reserved[i]))
      return false;
  for (const unsigned char *p = (const unsigned char *)value; *p; ++p)
    if (*p < 32 || *p > 126)
      return false;
  return true;
}

otlp_status_t otlp_http_request_start_with_socket(otlp_http_request_t **out, const struct otlp_http_url *url,
                                                  const char *agent, const otlp_http_header_t *headers, size_t count,
                                                  const uint8_t *body, size_t len, uint32_t connect_ms,
                                                  uint32_t read_ms, otlp_socket_t *socket) {
  if (!out || !url || (!body && len) || (!headers && count))
    return OTLP_ERR_NULL;
  *out = NULL;
  otlp_http_request_t *r = calloc(1, sizeof(*r));
  if (!r)
    return OTLP_ERR_NOMEM;
  otlp_status_t status = OTLP_ERR_NOMEM;
  if (!url->grpc) {
    status = socket ? gotel_http1_request_start_with_socket(&r->http1, url, agent, headers, count, body, len,
                                                            connect_ms, read_ms, socket)
                    : gotel_http1_request_start(&r->http1, url, agent, headers, count, body, len, connect_ms, read_ms);
    if (status != OTLP_OK) {
      free(r);
      return status;
    }
    *out = r;
    return OTLP_OK;
  }
  /* HTTP/2 sessions own their sockets; never donate an HTTP/1 connection. */
  otlp_socket_close(socket);
  if (len > UINT32_MAX || len > SIZE_MAX - 5 || count > 128) {
    free(r);
    return OTLP_ERR_OVERFLOW;
  }
  for (size_t i = 0; i < count; ++i)
    if (!metadata_valid(headers[i].name, headers[i].value)) {
      free(r);
      return OTLP_ERR_INVALID_ARGUMENT;
    }
  r->state = OTLP_HTTP_REQ_CONNECTING;
  r->grpc_status = -1;
  r->events = 2;
  r->connect_ms = connect_ms;
  r->read_ms = read_ms;
  r->started = r->progress = otlp_platform_now_mono_ms();
  r->send_len = len + 5;
  r->send = malloc(r->send_len);
  if (!r->send)
    goto fail;
  r->send[0] = 0;
  for (unsigned i = 0; i < 4; ++i)
    r->send[1 + i] = (uint8_t)((uint32_t)len >> (24 - i * 8));
  if (len)
    memcpy(r->send + 5, body, len);
  nghttp2_session_callbacks *callbacks = NULL;
  if (nghttp2_session_callbacks_new(&callbacks))
    goto fail;
  nghttp2_session_callbacks_set_on_header_callback(callbacks, header);
  nghttp2_session_callbacks_set_on_data_chunk_recv_callback(callbacks, data);
  nghttp2_session_callbacks_set_on_stream_close_callback(callbacks, close_stream);
  int rc = nghttp2_session_client_new(&r->session, callbacks, r);
  nghttp2_session_callbacks_del(callbacks);
  if (rc)
    goto fail;
  nghttp2_settings_entry settings = {NGHTTP2_SETTINGS_MAX_HEADER_LIST_SIZE, 16384};
  if (nghttp2_submit_settings(r->session, NGHTTP2_FLAG_NONE, &settings, 1))
    goto fail;
  char authority[OTLP_HTTP_HOST_MAX + 12], timeout[32];
  snprintf(authority, sizeof(authority), "%s%s%s:%u", strchr(url->host, ':') ? "[" : "", url->host,
           strchr(url->host, ':') ? "]" : "", (unsigned)url->port);
  snprintf(timeout, sizeof(timeout), "%uS", (unsigned)(read_ms / 1000 + (read_ms % 1000 != 0)));
  nghttp2_nv *fields = calloc(count + 9, sizeof(*fields));
  if (!fields)
    goto fail;
  size_t n = 0;
  fields[n++] = nv(":method", "POST");
  fields[n++] = nv(":scheme", url->tls ? "https" : "http");
  fields[n++] = nv(":authority", authority);
  fields[n++] = nv(":path", url->path);
  fields[n++] = nv("content-type", "application/grpc");
  fields[n++] = nv("te", "trailers");
  fields[n++] = nv("user-agent", agent ? agent : "c");
  if (read_ms)
    fields[n++] = nv("grpc-timeout", timeout);
  for (size_t i = 0; i < count; ++i)
    fields[n++] = nv(headers[i].name, headers[i].value);
  nghttp2_data_provider2 provider = {{0}, read_body};
  r->stream = nghttp2_submit_request2(r->session, NULL, fields, n, &provider, r);
  free(fields);
  if (r->stream < 0)
    goto fail;
  if (url->tls) {
    r->tls = gotel_tls_create(url->host);
    if (!r->tls || !gotel_tls_enable_h2(r->tls)) {
      status = OTLP_ERR_INVALID_ARGUMENT;
      goto fail;
    }
  }
  status = otlp_socket_connect(&r->socket, url->host, url->port);
  if (status != OTLP_OK)
    goto fail;
  *out = r;
  return OTLP_OK;
fail:
  otlp_http_request_free(r);
  return status;
}
otlp_status_t otlp_http_request_start(otlp_http_request_t **out, const struct otlp_http_url *url, const char *agent,
                                      const otlp_http_header_t *headers, size_t count, const uint8_t *body, size_t len,
                                      uint32_t connect_ms, uint32_t read_ms) {
  return otlp_http_request_start_with_socket(out, url, agent, headers, count, body, len, connect_ms, read_ms, NULL);
}
static otlp_status_t failure(otlp_http_request_t *r, otlp_status_t status) {
  if (status != OTLP_OK && status != OTLP_ERR_WOULDBLOCK)
    r->state = OTLP_HTTP_REQ_FAILED;
  return status;
}
otlp_status_t otlp_http_request_step(otlp_http_request_t *r) {
  if (!r)
    return OTLP_ERR_NULL;
  if (r->http1)
    return gotel_http1_request_step(r->http1);
  if (r->state == OTLP_HTTP_REQ_DONE || r->state == OTLP_HTTP_REQ_FAILED)
    return OTLP_OK;
  uint64_t now = otlp_platform_now_mono_ms();
  uint32_t limit = r->connected ? r->read_ms : r->connect_ms;
  if (limit && now - (r->connected ? r->progress : r->started) >= limit)
    return failure(r, OTLP_ERR_TIMEOUT);
  if (!r->connected) {
    otlp_status_t status = otlp_socket_finish_connect(r->socket);
    if (status != OTLP_OK)
      return failure(r, status);
    r->connected = true;
    r->progress = now;
    r->state = OTLP_HTTP_REQ_SENDING;
  }
  /* Bound work per tick. nghttp2 retains serialized output until the next send. */
  for (unsigned i = 0; i < 16; ++i) {
    if (r->wire_at == r->wire_len) {
      nghttp2_ssize n = nghttp2_session_mem_send2(r->session, &r->wire);
      if (n < 0)
        return failure(r, OTLP_ERR_PROTOCOL);
      r->wire_len = (size_t)n;
      r->wire_at = 0;
      if (!n)
        break;
    }
    size_t written = 0;
    otlp_status_t status =
        r->tls ? gotel_tls_write(r->tls, r->socket, r->wire + r->wire_at, r->wire_len - r->wire_at, &written)
               : otlp_socket_write(r->socket, r->wire + r->wire_at, r->wire_len - r->wire_at, &written);
    r->wire_at += written;
    if (written)
      r->progress = now;
    if (status != OTLP_OK || !written) {
      r->events = r->tls ? gotel_tls_events(r->tls) : 2;
      return failure(r, status == OTLP_OK ? OTLP_ERR_WOULDBLOCK : status);
    }
  }
  r->state = OTLP_HTTP_REQ_READING;
  uint8_t buffer[16384];
  size_t n = 0;
  otlp_status_t status = r->tls ? gotel_tls_read(r->tls, r->socket, buffer, sizeof(buffer), &n)
                                : otlp_socket_read(r->socket, buffer, sizeof(buffer), &n);
  r->events = 1 | ((r->wire_at < r->wire_len || nghttp2_session_want_write(r->session)) ? 2 : 0);
  if (r->tls && status == OTLP_ERR_WOULDBLOCK)
    r->events |= gotel_tls_events(r->tls);
  if (status != OTLP_OK)
    return failure(r, status);
  if (!n)
    return failure(r,
                   (r->tls ? gotel_tls_eof(r->tls) : otlp_socket_eof(r->socket)) ? OTLP_ERR_READ : OTLP_ERR_WOULDBLOCK);
  r->progress = now;
  if (nghttp2_session_mem_recv2(r->session, buffer, n) != (nghttp2_ssize)n) {
    if (r->too_large) {
      r->grpc_status = 8;
      r->retry_info = false;
      r->state = OTLP_HTTP_REQ_DONE;
      return OTLP_OK;
    }
    return failure(r, OTLP_ERR_PROTOCOL);
  }
  if (r->closed) {
    if (!r->invalid && r->status != 200 && r->status > 0 && r->grpc_status < 0) {
      r->grpc_status = (r->status == 429 || r->status == 502 || r->status == 503 || r->status == 504) ? 14 : 2;
      r->state = OTLP_HTTP_REQ_DONE;
      return OTLP_OK;
    }
    if (r->invalid || r->grpc_status < 0 || !r->content_type)
      return failure(r, OTLP_ERR_PROTOCOL);
    if (r->grpc_status == 0) {
      if (r->status != 200 || r->response_len < 5 || r->response[0])
        return failure(r, OTLP_ERR_PROTOCOL);
      uint32_t len = ((uint32_t)r->response[1] << 24) | ((uint32_t)r->response[2] << 16) |
                     ((uint32_t)r->response[3] << 8) | r->response[4];
      if (len != r->response_len - 5)
        return failure(r, OTLP_ERR_PROTOCOL);
    }
    r->state = OTLP_HTTP_REQ_DONE;
  }
  return OTLP_OK;
}
otlp_http_req_state_t otlp_http_request_state(const otlp_http_request_t *r) {
  return r->http1 ? gotel_http1_request_state(r->http1) : r->state;
}
int otlp_http_request_fd(const otlp_http_request_t *r) {
  return r->http1 ? gotel_http1_request_fd(r->http1) : otlp_socket_fd(r->socket);
}
int otlp_http_request_events(const otlp_http_request_t *r) {
  return r->http1 ? gotel_http1_request_events(r->http1)
                  : (r->state == OTLP_HTTP_REQ_DONE || r->state == OTLP_HTTP_REQ_FAILED ? 0 : r->events);
}
int otlp_http_request_http_status(const otlp_http_request_t *r) {
  if (r->http1)
    return gotel_http1_request_http_status(r->http1);
  if (!r->grpc_status)
    return 200;
  /* OTLP retryable gRPC codes: CANCELLED, DEADLINE_EXCEEDED, RESOURCE_EXHAUSTED,
   * ABORTED, OUT_OF_RANGE, UNAVAILABLE, DATA_LOSS. Other statuses are permanent. */
  switch (r->grpc_status) {
  case 1:
  case 4:
  case 10:
  case 11:
  case 14:
  case 15:
    return 503;
  case 8:
    return r->retry_info ? 503 : 400;
  default:
    return 400;
  }
}
uint32_t otlp_http_request_retry_after_ms(const otlp_http_request_t *r) {
  return r->http1 ? gotel_http1_request_retry_after_ms(r->http1) : r->retry_ms;
}
const uint8_t *otlp_http_request_body(const otlp_http_request_t *r, size_t *len) {
  if (r->http1)
    return gotel_http1_request_body(r->http1, len);
  if (len)
    *len = r->grpc_status == 0 ? r->response_len - 5 : 0;
  return r->grpc_status == 0 ? r->response + 5 : NULL;
}
otlp_socket_t *otlp_http_request_detach_socket(otlp_http_request_t *r) {
  return r->http1 ? gotel_http1_request_detach_socket(r->http1) : NULL;
}
void otlp_http_request_free(otlp_http_request_t *r) {
  if (!r)
    return;
  if (r->http1)
    gotel_http1_request_free(r->http1);
  nghttp2_session_del(r->session);
  gotel_tls_free(r->tls);
  otlp_socket_close(r->socket);
  free(r->send);
  free(r->response);
  free(r);
}
