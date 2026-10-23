/* Shared SQL source SHA256: 4960732c19c02c976b6ff9864780c665c9647da22414133dcecb4d7ca3974fd3 */
#define CGOTEL_SQL_STANDALONE 1
/* Shared SQL engine includes the clause-aware layout formatter. Keep this
 * bridge updated with engine changes so cgo invalidates its native cache. */
#include "../c-gotel/src/sql/main.c"
#include "../c-gotel/src/utf8.c"
