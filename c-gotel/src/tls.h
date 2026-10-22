#ifndef GOTEL_TLS_H
#define GOTEL_TLS_H
#include <otlp-c/status.h>
#include <stddef.h>
#include <stdint.h>
typedef struct otlp_socket otlp_socket_t;
typedef struct gotel_tls_store gotel_tls_store;
typedef struct gotel_tls gotel_tls;
otlp_status_t gotel_tls_store_create(const char *file, gotel_tls_store **out);
void gotel_tls_store_free(gotel_tls_store *store);
gotel_tls_store *gotel_tls_select(gotel_tls_store *store);
gotel_tls *gotel_tls_create(const char *hostname);
void gotel_tls_free(gotel_tls *tls);
otlp_status_t gotel_tls_write(gotel_tls *tls, otlp_socket_t *socket, const uint8_t *data, size_t len, size_t *written);
otlp_status_t gotel_tls_read(gotel_tls *tls, otlp_socket_t *socket, uint8_t *data, size_t cap, size_t *read);
int gotel_tls_eof(gotel_tls *tls);
int gotel_tls_events(gotel_tls *tls);
#endif
