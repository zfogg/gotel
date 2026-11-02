# Go tracers

Import instrumentation from `github.com/zfogg/gotel/go/tracers/<name>`.
Package names match the final path component. Alias imports when they share a
name with a client library, such as `gotelhttp` alongside `net/http`.

| Previous package | New package |
| --- | --- |
| `gormtrace` | `tracers/gorm` |
| `httptrace` | `tracers/http` |
| `mcptrace` | `tracers/mcp` |
| `pgxtrace` | `tracers/pgx` |
| `redistrace` | `tracers/redis` |
| `sqltrace` | `tracers/sql` |
| `temporaltrace` | `tracers/temporal` |
| `traceprocessor` | `tracers/processor` |

The old import paths have been removed. Public adapter APIs remain the same.

```go
import (
    "net/http"
    gotelhttp "github.com/zfogg/gotel/go/tracers/http"
)

func handler(mux *http.ServeMux) http.Handler {
    return (gotelhttp.Namer{Prefix: "catalog"}).Handler(mux, "catalog-api")
}
```
