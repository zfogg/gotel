package gotel

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// RecordError records bounded error metadata. The concrete message is omitted
// because it can contain credentials, SQL values, request bodies, or IDs.
func RecordError(span trace.Span, err error) {
	if span == nil || err == nil {
		return
	}
	typ := reflect.TypeOf(err)
	name := typ.String()
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Name() != "" {
		name = typ.PkgPath() + "." + typ.Name()
	}
	span.SetAttributes(attribute.String("error.type", name))
	span.SetStatus(codes.Error, "operation failed")
}

// ErrorType is useful for bounded logs and metrics that must not expose the
// original error message.
func ErrorType(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%T", err)
}

func IsCancellation(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
