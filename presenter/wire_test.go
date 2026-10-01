package presenter

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type wirePayload struct {
	Name string         `json:"name"`
	Tags []string       `json:"tags"`
	Meta map[string]int `json:"meta"`
	HTML string         `json:"html"`
}

// Pins the exact bytes presenter.JSON writes. Expectations change ONLY with a
// documented wire-format decision (see CHANGELOG), one line per difference.
func TestJSON_wireBytes(t *testing.T) {
	t.Parallel()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	JSON(rr, req, wirePayload{Name: "a", HTML: "<b>&</b>"})
	want := `{"name":"a","tags":[],"meta":{},"html":"<b>&</b>"}`
	if got := rr.Body.String(); got != want {
		t.Fatalf("body =\n%s\nwant\n%s", got, want)
	}
}

type bindDTO struct {
	Name string `json:"name"`
}

func (d *bindDTO) Bind(*http.Request) error { return nil }

func TestBind_wireSemantics(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		body    string
		wantErr bool
		want    string
	}{
		{name: "exact", body: `{"name":"x"}`, want: "x"},
		{name: "case mismatch is an unknown member", body: `{"NAME":"x"}`, want: ""},
		{name: "duplicate key rejected", body: `{"name":"a","name":"b"}`, wantErr: true, want: "a"},
		{name: "invalid utf8 rejected", body: "{\"name\":\"\xff\"}", wantErr: true, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			var d bindDTO
			err := Bind(req, &d)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if d.Name != tc.want {
				t.Fatalf("name = %q, want %q", d.Name, tc.want)
			}
		})
	}
}

func TestJSON_mapKeysAreSorted(t *testing.T) {
	t.Parallel()
	m := map[string]int{"z": 1, "a": 2, "m": 3, "k": 4, "b": 5, "y": 6, "c": 7, "x": 8, "d": 9}
	want := `{"a":2,"b":5,"c":7,"d":9,"k":4,"m":3,"x":8,"y":6,"z":1}`
	for range 50 {
		rr := httptest.NewRecorder()
		JSON(rr, httptest.NewRequest(http.MethodGet, "/", http.NoBody), m)
		if got := rr.Body.String(); got != want {
			t.Fatalf("map encoding is not deterministic: %s", got)
		}
	}
}

func TestJSON_marshalErrorYields500WithoutBody(t *testing.T) {
	t.Parallel()
	type bad struct {
		D time.Duration `json:"d"` // json/v2 has no default representation for Duration
	}
	rr := httptest.NewRecorder()
	JSON(rr, httptest.NewRequest(http.MethodGet, "/", http.NoBody), bad{D: time.Second})
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (never a 200 with an empty or truncated body)", rr.Code)
	}
	if rr.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", rr.Body.String())
	}
}

// Pins v2 behaviours documented in the CHANGELOG incompatibility table.
func TestJSON_documentedV2Semantics(t *testing.T) {
	t.Parallel()
	type payload struct {
		Count int     `json:"count,omitempty"`
		OK    bool    `json:"ok,omitempty"`
		Raw   []byte  `json:"raw"`
		Arr   [2]byte `json:"arr"`
	}
	rr := httptest.NewRecorder()
	JSON(rr, httptest.NewRequest(http.MethodGet, "/", http.NoBody), payload{})
	want := `{"count":0,"ok":false,"raw":"","arr":"AAA="}`
	if got := rr.Body.String(); got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func TestBind_documentedV2Semantics(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, body string }{
		{"empty body is an error (not io.EOF)", ""},
		{"trailing data rejected", `{"name":"x"} x`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var d bindDTO
			err := Bind(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body)), &d)
			if err == nil {
				t.Fatal("want error")
			}
			if errors.Is(err, io.EOF) {
				t.Fatal("v2 errors do not wrap io.EOF; consumers must not rely on it")
			}
		})
	}
}
