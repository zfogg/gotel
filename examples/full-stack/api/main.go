package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/zfogg/gotel/go/httptrace"
	"github.com/zfogg/gotel/go/otelhost"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://spaniel:4318"
	}
	runtime, err := otelhost.Start(ctx, otelhost.Config{
		ServiceName: "gotel-demo-api", Endpoint: endpoint,
		Protocol: otelhost.ProtocolHTTPProtobuf,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := runtime.Shutdown(shutdownCtx); err != nil {
			log.Print(err)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("POST /api/count", countWords)
	handler := (httptrace.Namer{Prefix: "demo.api"}).Handler(mux, "demo-api")
	server := &http.Server{Addr: ":8080", Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func countWords(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Text string `json:"text"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := decoder.Decode(&input); err != nil || strings.TrimSpace(input.Text) == "" {
		http.Error(w, "send JSON with nonempty text", http.StatusBadRequest)
		return
	}
	ctx, span := otel.Tracer("gotel-demo-api").Start(r.Context(), "demo.api.run_wordcount")
	defer span.End()
	parent := span.SpanContext()
	traceparent := fmt.Sprintf("00-%s-%s-%02x", parent.TraceID(), parent.SpanID(), byte(parent.TraceFlags()))
	command := exec.CommandContext(ctx, "/usr/local/bin/wordcount")
	command.Env = append(os.Environ(), "TRACEPARENT="+traceparent)
	command.Stdin = strings.NewReader(input.Text)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		span.RecordError(err)
		http.Error(w, "wordcount failed: "+stderr.String(), http.StatusBadGateway)
		return
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		span.RecordError(err)
		http.Error(w, "invalid wordcount output", http.StatusBadGateway)
		return
	}
	span.SetAttributes(attribute.Int("demo.word_count", count))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Words   int    `json:"words"`
		TraceID string `json:"trace_id"`
	}{Words: count, TraceID: parent.TraceID().String()})
}
