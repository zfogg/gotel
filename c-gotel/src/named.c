#include <gotel/gotel.h>
#include <string.h>
/* Immutable named query registry. Caller-owned descriptors avoid allocation,
 * locks and pointer lifetimes tied to prepared-statement instances. */
const gotel_query_t *gotel_query_find(const gotel_query_t *queries,size_t count,const char *sql) {
  if(!queries || !sql) return NULL;
  for(size_t i=0;i<count;++i)
    if(queries[i].sql && !strcmp(queries[i].sql,sql)) return &queries[i];
  return NULL;
}
