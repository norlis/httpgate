package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInterceptStatus_wireBytesHaveNoTrailingNewline(t *testing.T) {
	t.Parallel()
	h := InterceptStatus(WithIntercept(http.StatusNotFound))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/x", http.NoBody))
	body := rr.Body.String()
	if strings.HasSuffix(body, "\n") {
		t.Fatalf("intercept body uses json.Marshal (no newline); got trailing newline: %q", body)
	}
	if !strings.HasPrefix(body, `{"title":"Not Found","status":404,"instance":"/x","timestamp":"`) {
		t.Fatalf("unexpected prefix: %s", body)
	}
}
