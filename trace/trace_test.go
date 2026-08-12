package trace

import (
	"context"
	"errors"
	"regexp"
	"testing"
)

func TestParse(t *testing.T) {
	t.Parallel()
	valid := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

	tests := []struct {
		name        string
		traceparent string
		wantErr     bool
		wantTraceID string
		wantSpanID  string
	}{
		{"valid v00", valid, false, "4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7"},
		{"empty", "", true, "", ""},
		{"too short", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa-01", true, "", ""},
		{"uppercase hex rejected", "00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01", true, "", ""},
		{"version ff invalid", "ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", true, "", ""},
		{"version non-hex", "zz-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", true, "", ""},
		{"v00 with trailing data invalid", valid + "-extra", true, "", ""},
		{"future version base format ok", "cc-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", false, "4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7"},
		{"future version with suffix ok", "cc-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01-what-the-future-will-be-like", false, "4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7"},
		{"future version bad suffix separator", "cc-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01.broken", true, "", ""},
		{"all-zero trace id", "00-00000000000000000000000000000000-00f067aa0ba902b7-01", true, "", ""},
		{"all-zero span id", "00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01", true, "", ""},
		{"wrong separators", "00_4bf92f3577b34da6a3ce929d0e0e4736_00f067aa0ba902b7_01", true, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Parse(tt.traceparent)
			if tt.wantErr {
				if !errors.Is(err, ErrMalformed) {
					t.Fatalf("want ErrMalformed, got %v (ctx=%+v)", err, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.TraceID != tt.wantTraceID || got.SpanID != tt.wantSpanID {
				t.Fatalf("got %+v, want trace=%s span=%s", got, tt.wantTraceID, tt.wantSpanID)
			}
		})
	}
}

var (
	traceIDRe = regexp.MustCompile(`^[0-9a-f]{32}$`)
	spanIDRe  = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

func TestNew_GeneratesValidIdentifiers(t *testing.T) {
	t.Parallel()
	tc := New()
	if !traceIDRe.MatchString(tc.TraceID) {
		t.Fatalf("trace id %q is not 32 lowercase hex chars", tc.TraceID)
	}
	if !spanIDRe.MatchString(tc.SpanID) {
		t.Fatalf("span id %q is not 16 lowercase hex chars", tc.SpanID)
	}
	if other := New(); other.TraceID == tc.TraceID {
		t.Fatal("two generated trace ids must differ")
	}
}

func TestNewSpanID_GeneratesValidID(t *testing.T) {
	t.Parallel()
	if id := NewSpanID(); !spanIDRe.MatchString(id) {
		t.Fatalf("span id %q is not 16 lowercase hex chars", id)
	}
}

func TestTraceparent_RoundTrips(t *testing.T) {
	t.Parallel()
	tc := Context{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7"}
	header := tc.Traceparent()
	if header != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" {
		t.Fatalf("unexpected header: %s", header)
	}
	parsed, err := Parse(header)
	if err != nil {
		t.Fatalf("generated header must parse: %v", err)
	}
	if parsed != tc {
		t.Fatalf("round trip mismatch: %+v != %+v", parsed, tc)
	}
}

func TestContext_RoundTripsThroughContext(t *testing.T) {
	t.Parallel()
	tc := New()
	ctx := NewContext(context.Background(), tc)
	got, ok := FromContext(ctx)
	if !ok || got != tc {
		t.Fatalf("FromContext = %+v, %v; want %+v, true", got, ok, tc)
	}
	if _, ok := FromContext(context.Background()); ok {
		t.Fatal("empty context must not carry a trace context")
	}
}
