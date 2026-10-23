#include "tls.h"
#include "platform.h"
#include <openssl/err.h>
#include <openssl/ssl.h>
#include <openssl/x509v3.h>
#include <stdlib.h>
#ifdef _WIN32
#include <wincrypt.h>
#include <windows.h>
#elif defined(__APPLE__)
#include <Security/Security.h>
#endif
struct gotel_tls_store {
  SSL_CTX *context;
};
struct gotel_tls {
  SSL *ssl;
  int events;
  int eof;
  int require_h2;
};
static _Thread_local gotel_tls_store *selected;
gotel_tls_store *gotel_tls_select(gotel_tls_store *store) {
  gotel_tls_store *previous = selected;
  selected = store;
  return previous;
}
#if defined(_WIN32) || defined(__APPLE__)
static void add_der(X509_STORE *store, const unsigned char *data, size_t len) {
  if (len > LONG_MAX)
    return;
  X509 *cert = d2i_X509(NULL, &data, (long)len);
  if (cert) {
    (void)X509_STORE_add_cert(store, cert);
    X509_free(cert);
  }
  ERR_clear_error();
}
#endif
otlp_status_t gotel_tls_store_create(const char *file, gotel_tls_store **out) {
  *out = calloc(1, sizeof(**out));
  if (!*out)
    return OTLP_ERR_NOMEM;
  gotel_tls_store *s = *out;
  s->context = SSL_CTX_new(TLS_client_method());
  if (!s->context)
    goto fail;
  if (!SSL_CTX_set_min_proto_version(s->context, TLS1_2_VERSION))
    goto fail;
  SSL_CTX_set_verify(s->context, SSL_VERIFY_PEER, NULL);
  SSL_CTX_set_options(s->context, SSL_OP_NO_COMPRESSION | SSL_OP_NO_RENEGOTIATION);
  if (file && *file) {
    if (SSL_CTX_load_verify_locations(s->context, file, NULL) != 1)
      goto fail;
  } else {
    /* Also import native roots: OpenSSL's compiled paths may not exist in a packaged app. */
    int loaded = SSL_CTX_set_default_verify_paths(s->context);
#ifdef _WIN32
    HCERTSTORE roots = CertOpenSystemStoreA(0, "ROOT");
    if (roots) {
      const CERT_CONTEXT *cert = NULL;
      while ((cert = CertEnumCertificatesInStore(roots, cert)) != NULL)
        add_der(SSL_CTX_get_cert_store(s->context), cert->pbCertEncoded, cert->cbCertEncoded);
      CertCloseStore(roots, 0);
      loaded = 1;
    }
#elif defined(__APPLE__)
    CFArrayRef roots = NULL;
    if (SecTrustCopyAnchorCertificates(&roots) == errSecSuccess) {
      for (CFIndex i = 0; i < CFArrayGetCount(roots); ++i) {
        CFDataRef der = SecCertificateCopyData((SecCertificateRef)CFArrayGetValueAtIndex(roots, i));
        if (der) {
          add_der(SSL_CTX_get_cert_store(s->context), CFDataGetBytePtr(der), (size_t)CFDataGetLength(der));
          CFRelease(der);
        }
      }
      CFRelease(roots);
      loaded = 1;
    }
#endif
    if (!loaded)
      goto fail;
  }
  ERR_clear_error();
  return OTLP_OK;
fail:
  gotel_tls_store_free(s);
  *out = NULL;
  ERR_clear_error();
  return OTLP_ERR_INVALID_ARGUMENT;
}
void gotel_tls_store_free(gotel_tls_store *s) {
  if (s) {
    SSL_CTX_free(s->context);
    free(s);
  }
}
gotel_tls *gotel_tls_create(const char *hostname) {
  if (!selected)
    return NULL;
  gotel_tls *t = calloc(1, sizeof(*t));
  if (!t)
    return NULL;
  t->ssl = SSL_new(selected->context);
  BIO *in = BIO_new(BIO_s_mem()), *out = BIO_new(BIO_s_mem());
  if (!t->ssl || !in || !out) {
    BIO_free(in);
    BIO_free(out);
    gotel_tls_free(t);
    return NULL;
  }
  BIO_set_mem_eof_return(in, -1);
  BIO_set_mem_eof_return(out, -1);
  SSL_set_bio(t->ssl, in, out);
  SSL_set_connect_state(t->ssl);
  X509_VERIFY_PARAM *verify = SSL_get0_param(t->ssl);
  X509_VERIFY_PARAM_set_hostflags(verify, X509_CHECK_FLAG_NO_PARTIAL_WILDCARDS);
  /* IP literals must match IP SANs; DNS names use SNI and DNS SAN verification. */
  if (X509_VERIFY_PARAM_set1_ip_asc(verify, hostname) != 1) {
    ERR_clear_error();
    if (SSL_set1_host(t->ssl, hostname) != 1 || SSL_set_tlsext_host_name(t->ssl, hostname) != 1) {
      gotel_tls_free(t);
      return NULL;
    }
  }
  t->events = 2;
  return t;
}
void gotel_tls_free(gotel_tls *t) {
  if (t) {
    SSL_free(t->ssl);
    free(t);
  }
}
int gotel_tls_enable_h2(gotel_tls *t) {
  static const unsigned char protocols[] = {2, 'h', '2'};
  t->require_h2 = 1;
  return SSL_set_alpn_protos(t->ssl, protocols, sizeof(protocols)) == 0;
}
static otlp_status_t negotiated(gotel_tls *t) {
  if (t->require_h2) {
    const unsigned char *protocol = NULL;
    unsigned int len = 0;
    SSL_get0_alpn_selected(t->ssl, &protocol, &len);
    if (len != 2 || protocol[0] != 'h' || protocol[1] != '2')
      return OTLP_ERR_PROTOCOL;
  }
  return OTLP_OK;
}
static otlp_status_t send_pending(gotel_tls *t, otlp_socket_t *s) {
  BIO *bio = SSL_get_wbio(t->ssl);
  for (int i = 0; i < 16; ++i) {
    char *data;
    long len = BIO_get_mem_data(bio, &data);
    if (len <= 0)
      return OTLP_OK;
    size_t sent = 0;
    otlp_status_t st = otlp_socket_write(s, (const uint8_t *)data, (size_t)len, &sent);
    if (st != OTLP_OK) {
      t->events = 2;
      return st;
    }
    if (!sent) {
      t->events = 2;
      return OTLP_ERR_WOULDBLOCK;
    }
    unsigned char discard[4096];
    while (sent) {
      size_t n = sent > sizeof(discard) ? sizeof(discard) : sent;
      if (BIO_read(bio, discard, (int)n) != (int)n)
        return OTLP_ERR_WRITE;
      sent -= n;
    }
  }
  t->events = 2;
  return OTLP_ERR_WOULDBLOCK;
}
static otlp_status_t receive(gotel_tls *t, otlp_socket_t *s) {
  unsigned char data[16384];
  size_t n = 0;
  t->events = 1;
  otlp_status_t st = otlp_socket_read(s, data, sizeof(data), &n);
  if (st != OTLP_OK)
    return st;
  if (!n)
    return otlp_socket_eof(s) ? OTLP_ERR_READ : OTLP_ERR_WOULDBLOCK;
  return BIO_write(SSL_get_rbio(t->ssl), data, (int)n) == (int)n ? OTLP_OK : OTLP_ERR_NOMEM;
}
static otlp_status_t handshake(gotel_tls *t, otlp_socket_t *s) {
  for (int i = 0; i < 16; ++i) {
    otlp_status_t st = send_pending(t, s);
    if (st != OTLP_OK)
      return st;
    if (SSL_is_init_finished(t->ssl))
      return negotiated(t);
    ERR_clear_error();
    int rc = SSL_do_handshake(t->ssl);
    int error = rc == 1 ? SSL_ERROR_NONE : SSL_get_error(t->ssl, rc);
    st = send_pending(t, s);
    if (st != OTLP_OK)
      return st;
    if (rc == 1)
      return negotiated(t);
    if (error == SSL_ERROR_WANT_READ) {
      st = receive(t, s);
      if (st != OTLP_OK)
        return st;
    } else if (error != SSL_ERROR_WANT_WRITE)
      return OTLP_ERR_CONNECT;
  }
  return OTLP_ERR_WOULDBLOCK;
}
otlp_status_t gotel_tls_write(gotel_tls *t, otlp_socket_t *s, const uint8_t *data, size_t len, size_t *written) {
  *written = 0;
  otlp_status_t st = handshake(t, s);
  if (st != OTLP_OK)
    return st;
  ERR_clear_error();
  int rc = SSL_write_ex(t->ssl, data, len, written);
  if (rc != 1)
    return OTLP_ERR_WRITE;
  st = send_pending(t, s);
  return st == OTLP_ERR_WOULDBLOCK ? OTLP_OK : st;
}
otlp_status_t gotel_tls_read(gotel_tls *t, otlp_socket_t *s, uint8_t *data, size_t cap, size_t *read) {
  *read = 0;
  otlp_status_t st = handshake(t, s);
  if (st != OTLP_OK)
    return st;
  for (int i = 0; i < 16; ++i) {
    ERR_clear_error();
    int rc = SSL_read_ex(t->ssl, data, cap, read);
    if (rc == 1)
      return OTLP_OK;
    int error = SSL_get_error(t->ssl, rc);
    if (error == SSL_ERROR_ZERO_RETURN) {
      t->eof = 1;
      return OTLP_OK;
    }
    st = send_pending(t, s);
    if (st != OTLP_OK)
      return st;
    if (error == SSL_ERROR_WANT_READ) {
      st = receive(t, s);
      if (st != OTLP_OK)
        return st;
    } else if (error != SSL_ERROR_WANT_WRITE)
      return OTLP_ERR_READ;
  }
  return OTLP_ERR_WOULDBLOCK;
}
int gotel_tls_eof(gotel_tls *t) { return t->eof; }
int gotel_tls_events(gotel_tls *t) { return t->events; }
