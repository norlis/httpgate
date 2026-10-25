package presenter

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type nameDTO struct {
	Name string `json:"name"`
}

func (d *nameDTO) Bind(*http.Request) error { return nil }

func TestBind_bodyLimit(t *testing.T) {
	t.Parallel()
	const limit = 64
	pad := func(n int) string { // {"name":"xxx..."} with total length n
		fixed := len(`{"name":""}`)
		return `{"name":"` + strings.Repeat("x", n-fixed) + `"}`
	}
	cases := []struct {
		name       string
		body       string
		wantStatus int // 0 means success
	}{
		{name: "exactly at limit", body: pad(limit)},
		{name: "one byte over", body: pad(limit + 1), wantStatus: http.StatusRequestEntityTooLarge},
		{name: "malformed", body: `{"name":`, wantStatus: http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			var d nameDTO
			err := Bind(rr, req, &d, WithMaxBodyBytes(limit))
			if tc.wantStatus == 0 {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			var be *BindError
			if !errors.As(err, &be) {
				t.Fatalf("err = %T %v, want *BindError", err, err)
			}
			if be.Status != tc.wantStatus {
				t.Fatalf("Status = %d, want %d", be.Status, tc.wantStatus)
			}
			if tc.wantStatus == http.StatusRequestEntityTooLarge {
				if _, ok := errors.AsType[*http.MaxBytesError](err); !ok {
					t.Fatal("413 BindError must unwrap to *http.MaxBytesError")
				}
			}
		})
	}
}

func TestBind_defaultLimitIsOneMiB(t *testing.T) {
	t.Parallel()
	if DefaultMaxBodyBytes != 1<<20 {
		t.Fatalf("DefaultMaxBodyBytes = %d", DefaultMaxBodyBytes)
	}
	rr := httptest.NewRecorder()
	big := `{"name":"` + strings.Repeat("x", 1<<20) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(big))
	var d nameDTO
	var be *BindError
	if err := Bind(rr, req, &d); !errors.As(err, &be) || be.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("err = %v, want 413 BindError", err)
	}
}

type failingBinder struct{ nameDTO }

func (f *failingBinder) Bind(*http.Request) error { return errors.New("name required") }

func TestBind_binderErrorIs400(t *testing.T) {
	t.Parallel()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"x"}`))
	var be *BindError
	err := Bind(rr, req, &failingBinder{}, WithMaxBodyBytes(1024))
	if !errors.As(err, &be) || be.Status != http.StatusBadRequest || !strings.Contains(err.Error(), "name required") {
		t.Fatalf("err = %v, want 400 BindError wrapping the binder error", err)
	}
}

// TestBind_overflowClosesConnectionThroughWrappers pins the 413 path against
// a real server behind a ResponseWriter wrapper: MaxBytesReader's own
// requestTooLarge hook is a direct type assertion that wrappers hide, so Bind
// must request the close itself or net/http drains the rejected body.
func TestBind_overflowClosesConnectionThroughWrappers(t *testing.T) {
	t.Parallel()
	type plainWrapper struct{ http.ResponseWriter }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var d nameDTO
		if err := Bind(plainWrapper{w}, r, &d, WithMaxBodyBytes(64)); err != nil {
			if be, ok := errors.AsType[*BindError](err); ok {
				w.WriteHeader(be.Status)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	body := `{"name":"` + strings.Repeat("x", 100<<10) + `"}`
	res, err := http.Post(srv.URL, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", res.StatusCode)
	}
	if !res.Close {
		t.Fatal("413 response must carry Connection: close so the server does not drain the rejected body")
	}
}
