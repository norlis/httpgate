package middleware

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestShed_rejectsBeyondLimitAndRecovers(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{}, 2) // buffered: the third request must not block on it
	release := make(chan struct{})
	h := Shed(1)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		entered <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	}))

	var wg sync.WaitGroup
	first := httptest.NewRecorder()
	wg.Go(func() { h.ServeHTTP(first, httptest.NewRequest("GET", "/a", http.NoBody)) })
	<-entered // slot 1 occupied

	second := httptest.NewRecorder()
	h.ServeHTTP(second, httptest.NewRequest("GET", "/b", http.NoBody))
	if second.Code != http.StatusServiceUnavailable {
		t.Fatalf("second status = %d, want 503", second.Code)
	}
	if ra := second.Header().Get("Retry-After"); ra != "1" {
		t.Fatalf("Retry-After = %q, want 1", ra)
	}
	if ct := second.Header().Get("Content-Type"); ct != "application/problem+json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}

	close(release)
	wg.Wait()
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, want 200", first.Code)
	}

	third := httptest.NewRecorder()
	h.ServeHTTP(third, httptest.NewRequest("GET", "/c", http.NoBody))
	if third.Code == http.StatusServiceUnavailable {
		t.Fatal("slot was not released after the first request finished")
	}
}

func TestShed_releasesSlotOnPanic(t *testing.T) {
	t.Parallel()
	calls := 0
	log := slog.New(slog.DiscardHandler)
	// Recover sits outside Shed (recommended chain order) so the panic is
	// logged and turned into a 500 while Shed's defer still frees the slot.
	h := New(Recover(log), Shed(1)).Then(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			panic("boom")
		}
		w.WriteHeader(http.StatusOK)
	}))

	rr1 := httptest.NewRecorder()
	h.ServeHTTP(rr1, httptest.NewRequest("GET", "/", http.NoBody))
	if rr1.Code != http.StatusInternalServerError {
		t.Fatalf("panicking request status = %d, want 500", rr1.Code)
	}
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, httptest.NewRequest("GET", "/", http.NoBody))
	if rr2.Code != http.StatusOK {
		t.Fatalf("after panic status = %d, want 200: slot leaked", rr2.Code)
	}
}

func TestShed_panicsOnNonPositiveLimit(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("Shed(0) must panic at construction")
		}
	}()
	_ = Shed(0)
}
