package pgx

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/codes"
)

func TestWrappedPostgresErrorsKeepSQLStateWithoutMessages(t *testing.T) {
	for _, code := range []string{"22P02", "42601"} {
		native := &pgconn.PgError{Code: code, Message: "PRIVATE_DRIVER_VALUE",
			Detail: "PRIVATE_DRIVER_DETAIL", Hint: "PRIVATE_DRIVER_HINT"}
		wrapped := fmt.Errorf("PRIVATE_WRAPPER: %w", native)
		for _, failure := range []error{native, wrapped, fmt.Errorf("PRIVATE_OUTER: %w", wrapped),
			errors.Join(errors.New("PRIVATE_OTHER"), native)} {
			t.Run(code+fmt.Sprintf("/%T", failure), func(t *testing.T) {
				tracer, recorder := lifecycleCapture(t)
				original, message := failure, failure.Error()
				ctx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
				data := pgx.TraceQueryEndData{Err: failure}
				tracer.TraceQueryEnd(ctx, nil, data)
				if data.Err != original || failure.Error() != message {
					t.Fatal("telemetry changed the caller's error")
				}
				span := recorder.Ended()[0]
				if span.Status().Code != codes.Error {
					t.Fatal("native failure lost error status")
				}
				attrs := map[string]string{}
				for _, attr := range span.Attributes() {
					attrs[string(attr.Key)] = attr.Value.AsString()
				}
				if attrs["db.response.status_code"] != code || attrs["error.type"] != fmt.Sprintf("%T", original) {
					t.Fatal("SQLSTATE or original outer error type was lost")
				}
				if strings.Contains(fmt.Sprint(span.Attributes(), span.Events(), span.Status()), "PRIVATE_") {
					t.Fatal("native/wrapper error message leaked into telemetry")
				}
			})
		}
	}
}
