package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"regexp"
	"testing"

	"github.com/norlis/httpgate/trace"
)

// lastLine decodes the last emitted NDJSON line of buf.
func lastLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	var m map[string]any
	if err := json.Unmarshal(lines[len(lines)-1], &m); err != nil {
		t.Fatalf("log line is not valid JSON: %v\n%s", err, buf.String())
	}
	return m
}

var timestampRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)

func TestNew_StandardKeys(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	log := New(buf)
	log.Info("payment processed")

	m := lastLine(t, buf)
	ts, _ := m[KeyTimestamp].(string)
	if !timestampRe.MatchString(ts) {
		t.Fatalf("timestamp %q is not ISO 8601 UTC with milliseconds", ts)
	}
	if m[KeyLevel] != "info" {
		t.Fatalf("log.level = %v, want %q", m[KeyLevel], "info")
	}
	if m[KeyMessage] != "payment processed" {
		t.Fatalf("message = %v", m[KeyMessage])
	}
	for _, legacy := range []string{"time", "level", "msg"} {
		if _, ok := m[legacy]; ok {
			t.Fatalf("legacy slog key %q must not be emitted: %v", legacy, m)
		}
	}
}

func TestNew_LevelsAreLowercase(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	log := New(buf, WithLevel(slog.LevelDebug))

	tests := []struct {
		logFn func(string, ...any)
		want  string
	}{
		{log.Debug, "debug"},
		{log.Info, "info"},
		{log.Warn, "warn"},
		{log.Error, "error"},
	}
	for _, tt := range tests {
		tt.logFn("event")
		if m := lastLine(t, buf); m[KeyLevel] != tt.want {
			t.Fatalf("log.level = %v, want %q", m[KeyLevel], tt.want)
		}
	}
}

func TestNew_BaseFields(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	log := New(buf, WithService("payment-service", "1.14.2"), WithEnvironment("production"))
	log.Info("event")

	m := lastLine(t, buf)
	if m[KeyServiceName] != "payment-service" || m[KeyServiceVersion] != "1.14.2" || m[KeyEnvironment] != "production" {
		t.Fatalf("base fields missing or wrong: %v", m)
	}
}

func TestNew_BaseFieldsAbsentWhenUnset(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	New(buf).Info("event")

	m := lastLine(t, buf)
	for _, k := range []string{KeyServiceName, KeyServiceVersion, KeyEnvironment} {
		if _, ok := m[k]; ok {
			t.Fatalf("unset base field %q must be absent: %v", k, m)
		}
	}
}

func TestNew_InjectsTraceContext(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	log := New(buf)
	tc := trace.Context{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7"}
	log.InfoContext(trace.NewContext(context.Background(), tc), "event")

	m := lastLine(t, buf)
	if m[KeyTraceID] != tc.TraceID || m[KeySpanID] != tc.SpanID {
		t.Fatalf("trace fields not injected: %v", m)
	}
}

func TestNew_NoTraceFieldsWithoutContext(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	New(buf).InfoContext(context.Background(), "event")

	m := lastLine(t, buf)
	if _, ok := m[KeyTraceID]; ok {
		t.Fatalf("trace_id must be absent without trace context: %v", m)
	}
}

func TestNew_DebugSuppressedByDefault(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	New(buf).Debug("noise")
	if buf.Len() != 0 {
		t.Fatalf("debug must be off by default, got: %s", buf.String())
	}
}

func TestNew_WithPreservesTraceInjection(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	sub := New(buf).With(slog.String("component", "opa"))
	tc := trace.Context{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7"}
	sub.InfoContext(trace.NewContext(context.Background(), tc), "event")

	m := lastLine(t, buf)
	if m[KeyTraceID] != tc.TraceID || m[KeySpanID] != tc.SpanID {
		t.Fatalf("With() must preserve trace injection: %v", m)
	}
	if m["component"] != "opa" {
		t.Fatalf("With() attrs must be emitted: %v", m)
	}
}

func TestNew_WithGroupPreservesTraceInjectionNested(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	grouped := New(buf).WithGroup("req")
	tc := trace.Context{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7"}
	grouped.InfoContext(trace.NewContext(context.Background(), tc), "event", slog.String("k", "v"))

	m := lastLine(t, buf)
	// Documented caveat: after WithGroup, injected trace fields land inside
	// the group. This test pins that behavior so a change is deliberate.
	g, ok := m["req"].(map[string]any)
	if !ok {
		t.Fatalf("expected req group: %v", m)
	}
	if g[KeyTraceID] != tc.TraceID || g["k"] != "v" {
		t.Fatalf("grouped attrs/trace mismatch: %v", m)
	}
}

// TestNew_UserAttrNamedTimeDoesNotPanic pins the fix for a panic in
// replaceAttr: a user attribute literally named "time" (slog.TimeKey) used to
// be passed unguarded to a.Value.Time(), which panics when the value isn't a
// time.Time. The standard timestamp field must still be emitted correctly.
func TestNew_UserAttrNamedTimeDoesNotPanic(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	log := New(buf)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("New(...).Info panicked on user attr named %q: %v", slog.TimeKey, r)
		}
	}()
	log.Info("event", slog.String("time", "x"))

	m := lastLine(t, buf)
	ts, _ := m[KeyTimestamp].(string)
	if !timestampRe.MatchString(ts) {
		t.Fatalf("timestamp %q is not ISO 8601 UTC with milliseconds: %v", ts, m)
	}
}

func BenchmarkHandler(b *testing.B) {
	log := New(io.Discard, WithService("bench", "1.0.0"), WithEnvironment("test"))
	ctx := trace.NewContext(context.Background(), trace.New())
	b.ReportAllocs()
	for b.Loop() {
		log.InfoContext(ctx, "request completed", slog.Int(KeyHTTPResponseStatusCode, 200))
	}
}
