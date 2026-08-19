package logging

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/norlis/httpgate/trace"
)

// BenchmarkHandler_Handle guards the hot path: Handle runs on every log line
// of every service, so the duration mirror must not cost allocations on the
// majority case that carries no duration at all.
func BenchmarkHandler_Handle(b *testing.B) {
	log := New(io.Discard)
	plain := context.Background()
	traced := trace.NewContext(plain, trace.Context{
		TraceID: "4bf92f3577b34da6a3ce929d0e0e4736",
		SpanID:  "00f067aa0ba902b7",
	})

	b.Run("no_duration", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			log.InfoContext(
				plain, "request completed",
				slog.String(KeyHTTPRequestMethod, "GET"),
				slog.String(KeyURLPath, "/api/entry"),
				slog.Int(KeyHTTPResponseStatusCode, 200),
			)
		}
	})

	b.Run("with_duration", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			log.InfoContext(
				plain, "request completed",
				slog.String(KeyHTTPRequestMethod, "GET"),
				slog.String(KeyURLPath, "/api/entry"),
				slog.Int(KeyHTTPResponseStatusCode, 200),
				slog.Int64(KeyEventDuration, int64(65484041*time.Nanosecond)),
			)
		}
	})

	b.Run("trace_and_duration", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			log.InfoContext(
				traced, "request completed",
				slog.String(KeyHTTPRequestMethod, "GET"),
				slog.String(KeyURLPath, "/api/entry"),
				slog.Int(KeyHTTPResponseStatusCode, 200),
				slog.Int64(KeyEventDuration, int64(65484041*time.Nanosecond)),
			)
		}
	})
}
