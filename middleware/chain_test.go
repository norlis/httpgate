package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func writingMW(label string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("<" + label))
			next.ServeHTTP(w, r)
			_, _ = w.Write([]byte(label + ">"))
		})
	}
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
}

func TestChain_Then_order(t *testing.T) {
	t.Parallel()
	c := New(writingMW("a"), writingMW("b"), writingMW("c"))
	rr := httptest.NewRecorder()
	c.Then(okHandler()).ServeHTTP(rr, httptest.NewRequest("GET", "/", http.NoBody))
	body, _ := io.ReadAll(rr.Body)
	if got := string(body); got != "<a<b<cokc>b>a>" {
		t.Fatalf("order = %q, want <a<b<cokc>b>a>", got)
	}
}

func TestChain_New_defensiveCopy(t *testing.T) {
	t.Parallel()
	mws := []Middleware{writingMW("a"), writingMW("b")}
	c := New(mws...)
	mws[0] = writingMW("REPLACED")
	rr := httptest.NewRecorder()
	c.Then(okHandler()).ServeHTTP(rr, httptest.NewRequest("GET", "/", http.NoBody))
	body, _ := io.ReadAll(rr.Body)
	if got := string(body); !strings.HasPrefix(got, "<a<b") {
		t.Fatalf("mutation leaked into Chain: body = %q", got)
	}
}

func TestChain_Then_nilDefaultsToDefaultServeMux(t *testing.T) {
	t.Parallel()
	c := New()
	h := c.Then(nil)
	if h == nil {
		t.Fatal("Then(nil) returned nil")
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/__nonexistent__", http.NoBody))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 from DefaultServeMux", rr.Code)
	}
}

func TestChain_Append_returnsNewChain(t *testing.T) {
	t.Parallel()
	base := New(writingMW("a"))
	extended := base.Append(writingMW("b"))

	rr1 := httptest.NewRecorder()
	base.Then(okHandler()).ServeHTTP(rr1, httptest.NewRequest("GET", "/", http.NoBody))
	body1, _ := io.ReadAll(rr1.Body)
	if string(body1) != "<aoka>" {
		t.Fatalf("base mutated: body = %q, want <aoka>", body1)
	}

	rr2 := httptest.NewRecorder()
	extended.Then(okHandler()).ServeHTTP(rr2, httptest.NewRequest("GET", "/", http.NoBody))
	body2, _ := io.ReadAll(rr2.Body)
	if string(body2) != "<a<bokb>a>" {
		t.Fatalf("extended chain order wrong: body = %q", body2)
	}
}

func TestChain_Extend_concatenates(t *testing.T) {
	t.Parallel()
	a := New(writingMW("a"))
	b := New(writingMW("b"), writingMW("c"))
	combined := a.Extend(b)
	rr := httptest.NewRecorder()
	combined.Then(okHandler()).ServeHTTP(rr, httptest.NewRequest("GET", "/", http.NoBody))
	body, _ := io.ReadAll(rr.Body)
	if string(body) != "<a<b<cokc>b>a>" {
		t.Fatalf("body = %q, want <a<b<cokc>b>a>", body)
	}
}

func TestChain_ThenFunc(t *testing.T) {
	t.Parallel()
	c := New(writingMW("a"))
	rr := httptest.NewRecorder()
	c.ThenFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("fn"))
	}).ServeHTTP(rr, httptest.NewRequest("GET", "/", http.NoBody))
	body, _ := io.ReadAll(rr.Body)
	if string(body) != "<afna>" {
		t.Fatalf("body = %q, want <afna>", body)
	}
}

func TestChain_concurrentThen(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	counting := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			next.ServeHTTP(w, r)
		})
	}
	c := New(counting)
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			rr := httptest.NewRecorder()
			c.Then(okHandler()).ServeHTTP(rr, httptest.NewRequest("GET", "/", http.NoBody))
		})
	}
	wg.Wait()
	if got := calls.Load(); got != 100 {
		t.Fatalf("call count = %d, want 100", got)
	}
}
