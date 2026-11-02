#pragma once

#include <otlp-c/status.h>
#include <stddef.h>
#include <stdint.h>
typedef struct otlp_socket otlp_socket_t;

typedef struct gotel_tls_store gotel_tls_store;

typedef struct gotel_tls gotel_tls;

/** Load the TLS trust store from a PEM file or system roots. */
otlp_status_t gotel_tls_store_create(const char *file, gotel_tls_store **out);

/** Release the TLS trust store. */
void gotel_tls_store_free(gotel_tls_store *store);

/** Select a trust store for the current thread and return the previous store. */
gotel_tls_store *gotel_tls_select(gotel_tls_store *store);

/** Create TLS connection state for the supplied hostname. */
gotel_tls *gotel_tls_create(const char *hostname);

/** Configure HTTP/2 protocol negotiation for the TLS connection. */
int gotel_tls_enable_h2(gotel_tls *tls);

/** Release TLS connection state. */
void gotel_tls_free(gotel_tls *tls);

/** Write plaintext through the TLS connection and report bytes consumed. */
otlp_status_t gotel_tls_write(gotel_tls *tls, otlp_socket_t *socket, const uint8_t *data, size_t len, size_t *written);

/** Read decrypted bytes from the TLS connection. */
otlp_status_t gotel_tls_read(gotel_tls *tls, otlp_socket_t *socket, uint8_t *data, size_t cap, size_t *read);

/** Return whether the TLS connection reached end of stream. */
int gotel_tls_eof(gotel_tls *tls);

/** Return the socket events needed to progress the TLS connection. */
int gotel_tls_events(gotel_tls *tls);
