// Package gormtrace instruments GORM operations with stable names and
// sanitized SQL templates.
package gormtrace

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zfogg/gotel"
	"github.com/zfogg/gotel/sqltrace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

// Config controls one GORM plugin instance.
type Config struct {
	// Name identifies the GORM plugin. It defaults to gotel:sql.
	Name string
	// Scope is the OpenTelemetry instrumentation scope.
	Scope string
	// Namer validates and prefixes every database span name.
	Namer sqltrace.Namer
	// System is db.system.name, such as postgresql or duckdb. When empty, the
	// GORM dialector name is used.
	System string
	// Namespace is the logical database name recorded as db.namespace.
	Namespace string
	// Caller may return a stable simple operation name when the context does not
	// carry one. sqltrace.CallerNamer constructs the common implementation.
	Caller func() string
	// Dialect selects SQL sanitization rules. It is inferred from System when
	// empty and otherwise falls back to generic SQL.
	Dialect sqltrace.Dialect
	// Sanitize customizes the safe SQL template policy. Dialect is filled from
	// the Dialect field when omitted.
	Sanitize sqltrace.SanitizeOptions
	// SpanKind defaults to client. INTERNAL is useful for embedded databases
	// such as DuckDB that execute in the application process.
	SpanKind trace.SpanKind
	// Observe receives bounded operation/result values and elapsed time after
	// each operation. It is suitable for application-owned metrics.
	Observe sqltrace.Observer
}

type plugin struct {
	config Config
	tracer trace.Tracer
}

// New returns a GORM plugin. Use db.Use(gormtrace.New(config)).
func New(config Config) gorm.Plugin {
	if config.Name == "" {
		config.Name = "gotel:sql"
	}
	if config.Scope == "" {
		config.Scope = strings.ReplaceAll(strings.Trim(config.Namer.Prefix, "."), ".", "/")
	}
	if config.Dialect == "" {
		config.Dialect = sqltrace.Dialect(strings.ToLower(config.System))
	}
	if config.Sanitize.Dialect == "" {
		config.Sanitize = sqltrace.DefaultSanitizeOptions(config.Dialect)
	}
	if config.SpanKind == trace.SpanKindUnspecified {
		config.SpanKind = trace.SpanKindClient
	}
	return &plugin{config: config, tracer: otel.Tracer(config.Scope)}
}

func (p *plugin) Name() string { return p.config.Name }

func (p *plugin) Initialize(db *gorm.DB) error {
	register := func(op string, before, after func(*gorm.DB)) error {
		key := "gotel:" + op
		switch op {
		case "query":
			return join(db.Callback().Query().Before("gorm:query").Register(key+":before", before), db.Callback().Query().After("gorm:query").Register(key+":after", after))
		case "create":
			return join(db.Callback().Create().Before("gorm:create").Register(key+":before", before), db.Callback().Create().After("gorm:create").Register(key+":after", after))
		case "update":
			return join(db.Callback().Update().Before("gorm:update").Register(key+":before", before), db.Callback().Update().After("gorm:update").Register(key+":after", after))
		case "delete":
			return join(db.Callback().Delete().Before("gorm:delete").Register(key+":before", before), db.Callback().Delete().After("gorm:delete").Register(key+":after", after))
		case "row":
			return join(db.Callback().Row().Before("gorm:row").Register(key+":before", before), db.Callback().Row().After("gorm:row").Register(key+":after", after))
		default:
			return join(db.Callback().Raw().Before("gorm:raw").Register(key+":before", before), db.Callback().Raw().After("gorm:raw").Register(key+":after", after))
		}
	}
	for _, op := range []string{"query", "create", "update", "delete", "row", "raw"} {
		if err := register(op, p.before(op), p.after(op)); err != nil {
			return err
		}
	}
	return nil
}

type state struct {
	span    trace.Span
	started time.Time
	op      string
	name    string
}

func (p *plugin) before(op string) func(*gorm.DB) {
	return func(db *gorm.DB) {
		ctx := db.Statement.Context
		if ctx == nil {
			ctx = context.Background()
		}
		if sqltrace.TracingDisabled(ctx) {
			return
		}
		name := sqltrace.NameFromContext(ctx)
		if name == "" && p.config.Caller != nil {
			if simple := p.config.Caller(); simple != "" {
				name = p.config.Namer.Name(simple)
			}
		}
		if name == "" {
			name = p.config.Namer.MustName(fallbackName(op))
		}
		ctx, span := p.tracer.Start(ctx, name, trace.WithSpanKind(p.config.SpanKind))
		db.Statement.Context = ctx
		db.InstanceSet(p.config.Name+":state", state{span: span, started: time.Now(), op: op, name: name})
	}
}

func (p *plugin) after(op string) func(*gorm.DB) {
	return func(db *gorm.DB) {
		value, ok := db.InstanceGet(p.config.Name + ":state")
		if !ok {
			return
		}
		st, ok := value.(state)
		if !ok {
			return
		}
		metadata := sqltrace.InspectWithOptions(p.config.Namer, "", db.Statement.SQL.String(), db.Statement.Vars, p.config.Sanitize)
		// Preserve the name selected before GORM built SQL, but attach the final
		// sanitized template and actual bind count.
		attrs := metadata.Attributes(p.system(db), p.config.Namespace)
		if db.Statement.Table != "" {
			attrs = append(attrs, attribute.String("db.collection.name", db.Statement.Table))
		}
		if db.RowsAffected >= 0 {
			attrs = append(attrs, attribute.Int64("db.response.returned_rows", db.RowsAffected))
		}
		result := "ok"
		err := db.Error
		if err == gorm.ErrRecordNotFound {
			err = nil
			result = "not_found"
		} else if err != nil {
			result = "error"
			attrs = append(attrs, attribute.String("error.type", fmt.Sprintf("%T", err)))
		}
		st.span.SetAttributes(attrs...)
		gotel.End(st.span, err)
		if p.config.Observe != nil {
			p.config.Observe(db.Statement.Context, op, result, time.Since(st.started))
		}
	}
}

func fallbackName(op string) string {
	switch op {
	case "query":
		return "Query"
	case "create":
		return "Create"
	case "update":
		return "Update"
	case "delete":
		return "Delete"
	case "row":
		return "Row"
	case "raw":
		return "Raw"
	default:
		return "Unknown"
	}
}

func (p *plugin) system(db *gorm.DB) string {
	if p.config.System != "" {
		return p.config.System
	}
	if db != nil && db.Dialector != nil {
		return db.Dialector.Name()
	}
	return ""
}

func join(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
