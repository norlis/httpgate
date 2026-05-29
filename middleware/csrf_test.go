package middleware

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// okHandler200 returns 200 OK with body "ok".
func okHandler200() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
}

func TestCSRFProtect_safeMethodsAlwaysAllowed(t *testing.T) {
	t.Parallel()
	mw := CSRFProtect()
	h := mw(okHandler200())
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		req := httptest.NewRequest(m, "/x", http.NoBody)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", m, rr.Code)
		}
	}
}

func TestCSRFProtect_sameOriginPostAllowed(t *testing.T) {
	t.Parallel()
	h := CSRFProtect()(okHandler200())
	req := httptest.NewRequest(http.MethodPost, "/x", http.NoBody)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

func TestCSRFProtect_crossOriginPostRejectedWith403(t *testing.T) {
	t.Parallel()
	h := CSRFProtect()(okHandler200())
	req := httptest.NewRequest(http.MethodPost, "/x", http.NoBody)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
	body, _ := io.ReadAll(rr.Body)
	if len(body) == 0 {
		t.Fatal("403 body should not be empty")
	}
}

func TestCSRFProtect_trustedOriginAllowed(t *testing.T) {
	t.Parallel()
	h := CSRFProtect(WithTrustedOrigin("https://app.example.com"))(okHandler200())
	req := httptest.NewRequest(http.MethodPost, "/x", http.NoBody)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "https://app.example.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (trusted origin)", rr.Code)
	}
}

func TestCSRFProtect_bypassPatternAllowed(t *testing.T) {
	t.Parallel()
	h := CSRFProtect(WithBypassPattern("/webhooks/"))(okHandler200())
	req := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", http.NoBody)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (bypass pattern)", rr.Code)
	}
}

func TestCSRFProtect_customDenyHandler(t *testing.T) {
	t.Parallel()
	deny := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("nope"))
	})
	h := CSRFProtect(WithDenyHandler(deny))(okHandler200())
	req := httptest.NewRequest(http.MethodPost, "/x", http.NoBody)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418 (custom deny handler)", rr.Code)
	}
	body, _ := io.ReadAll(rr.Body)
	if string(body) != "nope" {
		t.Fatalf("body = %q, want 'nope'", body)
	}
}

func TestCSRFProtect_panicsOnInvalidTrustedOrigin(t *testing.T) {
	t.Parallel()
	defer func() {
		rvr := recover()
		if rvr == nil {
			t.Fatal("expected panic for invalid trusted origin")
		}
		err, ok := rvr.(error)
		if !ok {
			t.Fatalf("panic value is not error: %T", rvr)
		}
		if !strings.Contains(err.Error(), "invalid trusted origin") {
			t.Fatalf("error should mention 'invalid trusted origin': %v", err)
		}
	}()
	_ = CSRFProtect(WithTrustedOrigin("not a valid origin"))
}

func TestCSRFProtect_chainComposes(t *testing.T) {
	t.Parallel()
	chain := New(
		CSRFProtect(WithTrustedOrigin("https://app.example.com")),
	)
	h := chain.ThenFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("reached"))
	})
	req := httptest.NewRequest(http.MethodPost, "/x", http.NoBody)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	body, _ := io.ReadAll(rr.Body)
	if string(body) != "reached" {
		t.Fatalf("body = %q", body)
	}
}

var _ = errors.New
