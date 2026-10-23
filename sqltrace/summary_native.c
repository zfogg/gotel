/* Shared SQL source SHA256: dcf062bb72e62fae51bd7b9b605d9f902ef375c4dcb0b641faa58776e293d3a5 */
#define CGOTEL_SQL_STANDALONE 1
/* Shared SQL engine includes the clause-aware layout formatter. Keep this
 * bridge updated with engine changes so cgo invalidates its native cache. */
#include "../c-gotel/src/sql/main.c"
#include "../c-gotel/src/utf8.c"
