package logging

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/norlis/httpgate/trace"
)

func TestHandler_DurationHumanMirror(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	New(buf).Info("request completed", slog.Int64(KeyEventDuration, 65484041))

	m := lastLine(t, buf)
	if m[KeyEventDurationHuman] != "65.484041ms" {
		t.Fatalf("%s = %v, want %q", KeyEventDurationHuman, m[KeyEventDurationHuman], "65.484041ms")
	}
	if d, ok := m[KeyEventDuration].(float64); !ok || int64(d) != 65484041 {
		t.Fatalf("%s must survive untouched as nanoseconds: %v", KeyEventDuration, m)
	}
}

func TestHandler_DurationHumanFromKindDuration(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	New(buf).Info("request completed", slog.Duration(KeyEventDuration, 5*time.Millisecond))

	m := lastLine(t, buf)
	if m[KeyEventDurationHuman] != "5ms" {
		t.Fatalf("%s = %v, want %q", KeyEventDurationHuman, m[KeyEventDurationHuman], "5ms")
	}
}

func TestHandler_NoDurationNoMirror(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	New(buf).Info("payment processed", slog.String("payment.id", "p-1"))

	m := lastLine(t, buf)
	if _, ok := m[KeyEventDurationHuman]; ok {
		t.Fatalf("%s must be absent when there is no duration: %v", KeyEventDurationHuman, m)
	}
}

// Zero and negative durations are mirrored as-is, never filtered: a mirror
// that second-guesses its source stops being a mirror.
func TestHandler_ZeroAndNegativeDurationsNotFiltered(t *testing.T) {
	t.Parallel()
	for ns, want := range map[int64]string{0: "0s", -5_000_000: "-5ms"} {
		buf := &bytes.Buffer{}
		New(buf).Info("request completed", slog.Int64(KeyEventDuration, ns))

		if m := lastLine(t, buf); m[KeyEventDurationHuman] != want {
			t.Fatalf("%s for %d ns = %v, want %q", KeyEventDurationHuman, ns, m[KeyEventDurationHuman], want)
		}
	}
}

// A mistyped event.duration must be passed through untouched: logging never
// breaks the request over a bad attr.
func TestHandler_UnsupportedDurationKind(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	New(buf).Info("request completed", slog.String(KeyEventDuration, "5ms"))

	m := lastLine(t, buf)
	if _, ok := m[KeyEventDurationHuman]; ok {
		t.Fatalf("%s must not be produced from an unsupported kind: %v", KeyEventDurationHuman, m)
	}
	if m[KeyEventDuration] != "5ms" {
		t.Fatalf("%s must survive untouched: %v", KeyEventDuration, m)
	}
}

// Documented limitation: only a top-level record attr is mirrored. A duration
// pre-bound with With lives on the inner handler, out of the record's reach.
func TestHandler_PreboundDurationNotMirrored(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	New(buf).With(slog.Int64(KeyEventDuration, 65484041)).Info("request completed")

	m := lastLine(t, buf)
	if _, ok := m[KeyEventDurationHuman]; ok {
		t.Fatalf("pre-bound duration must not be mirrored: %v", m)
	}
	if _, ok := m[KeyEventDuration]; !ok {
		t.Fatalf("pre-bound %s must still be emitted: %v", KeyEventDuration, m)
	}
}

func TestHandler_TraceAndDurationSingleClone(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	tc := trace.Context{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7"}
	New(buf).InfoContext(
		trace.NewContext(context.Background(), tc),
		"request completed",
		slog.Int64(KeyEventDuration, 65484041),
	)

	m := lastLine(t, buf)
	for k, want := range map[string]any{
		KeyTraceID:            tc.TraceID,
		KeySpanID:             tc.SpanID,
		KeyEventDuration:      float64(65484041),
		KeyEventDurationHuman: "65.484041ms",
	} {
		if m[k] != want {
			t.Fatalf("%s = %v, want %v (full line: %v)", k, m[k], want, m)
		}
	}
}
