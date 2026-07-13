package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestWrapWriter_UnwrapExposesFlush ensures the wrapper does not hide the
// underlying writer's capabilities: http.NewResponseController must reach
// Flush through Unwrap (Go 1.20+). Without it, SSE/streaming break silently.
func TestWrapWriter_UnwrapExposesFlush(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	ww := WrapWriter(rec)

	if err := http.NewResponseController(ww).Flush(); err != nil {
		t.Fatalf("Flush through wrapper: %v", err)
	}
	if !rec.Flushed {
		t.Fatal("underlying recorder was not flushed via the wrapper")
	}
}

func TestWrapWriter_UnwrapReturnsUnderlying(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	ww := WrapWriter(rec)

	u, ok := ww.(interface{ Unwrap() http.ResponseWriter })
	if !ok {
		t.Fatal("WrapWriter result does not implement Unwrap")
	}
	if u.Unwrap() != rec {
		t.Fatal("Unwrap did not return the underlying writer")
	}
}
