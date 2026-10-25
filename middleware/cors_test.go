package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/norlis/httpgate/logging"
)

// Characterization tests for CORS/AllowAll (proposal C1). Behaviour fixed here
// is what the middleware did before the corsEngine/corsAdapter collapse, except
// where a test says otherwise (B1).

type corsCall struct {
	called bool
	rec    *httptest.ResponseRecorder
}

func serveCORS(mw Middleware, method, origin string, set func(h http.Header)) corsCall {
	c := corsCall{rec: httptest.NewRecorder()}
	req := httptest.NewRequest(method, "/r", http.NoBody)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if set != nil {
		set(req.Header)
	}
	mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		c.called = true
		w.WriteHeader(http.StatusTeapot)
	})).ServeHTTP(c.rec, req)
	return c
}

func preflight(method, headers string) func(h http.Header) {
	return func(h http.Header) {
		h.Set("Access-Control-Request-Method", method)
		if headers != "" {
			h.Set("Access-Control-Request-Headers", headers)
		}
	}
}

func TestCORS_PreflightAllowed(t *testing.T) {
	t.Parallel()
	mw := CORS(CORSOptions{
		AllowedOrigins: []string{"https://app.example.com"},
		AllowedMethods: []string{"put"},
		AllowedHeaders: []string{"x-token"},
		ExposedHeaders: []string{"x-trace"},
		MaxAge:         600,
	})
	c := serveCORS(mw, http.MethodOptions, "https://app.example.com", preflight("PUT", "X-Token, content-type"))
	h := c.rec.Header()
	if c.called {
		t.Fatal("preflight must not reach next without OptionsPassthrough")
	}
	if c.rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", c.rec.Code)
	}
	// content-type is not in AllowedHeaders → preflight rejected on headers.
	if h.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("headers not allowed must reject: %v", h)
	}

	c = serveCORS(mw, http.MethodOptions, "https://app.example.com", preflight("put", "x-token"))
	h = c.rec.Header()
	want := map[string]string{
		"Access-Control-Allow-Origin":  "https://app.example.com",
		"Access-Control-Allow-Methods": "PUT",
		"Access-Control-Allow-Headers": "x-token",
		"Access-Control-Max-Age":       "600",
	}
	for k, v := range want {
		if got := h.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if h.Get("Access-Control-Allow-Credentials") != "" || h.Get("Access-Control-Expose-Headers") != "" {
		t.Errorf("credentials/expose must not appear on this preflight: %v", h)
	}
	if vary := strings.Join(h.Values("Vary"), ","); vary != "Origin,Access-Control-Request-Method,Access-Control-Request-Headers" {
		t.Errorf("Vary = %q", vary)
	}
}

func TestCORS_PreflightRejectedStillAnswers200WithoutCORSHeaders(t *testing.T) {
	t.Parallel()
	mw := CORS(CORSOptions{AllowedOrigins: []string{"https://ok.example"}, AllowedMethods: []string{"GET"}})
	cases := map[string]corsCall{
		"empty origin":       serveCORS(mw, http.MethodOptions, "", preflight("GET", "")),
		"origin not allowed": serveCORS(mw, http.MethodOptions, "https://evil.example", preflight("GET", "")),
		"method not allowed": serveCORS(mw, http.MethodOptions, "https://ok.example", preflight("DELETE", "")),
	}
	for name, c := range cases {
		if c.called || c.rec.Code != http.StatusOK || c.rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s: called=%v status=%d ACAO=%q", name, c.called, c.rec.Code, c.rec.Header().Get("Access-Control-Allow-Origin"))
		}
		if len(c.rec.Header().Values("Vary")) != 3 {
			t.Errorf("%s: Vary must always be set on preflight: %v", name, c.rec.Header().Values("Vary"))
		}
	}
}

func TestCORS_OptionsPassthroughReachesNext(t *testing.T) {
	t.Parallel()
	mw := CORS(CORSOptions{OptionsPassthrough: true})
	c := serveCORS(mw, http.MethodOptions, "https://a.example", preflight("GET", ""))
	if !c.called || c.rec.Code != http.StatusTeapot {
		t.Fatalf("called=%v status=%d; next must handle OPTIONS", c.called, c.rec.Code)
	}
	if c.rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("ACAO = %q", c.rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORS_ActualRequest(t *testing.T) {
	t.Parallel()
	mw := CORS(CORSOptions{
		AllowedOrigins:   []string{"https://App.Example.com", "https://*.cdn.example"},
		ExposedHeaders:   []string{"x-trace", "x-other"},
		AllowCredentials: true,
	})
	t.Run("plain origin, case-insensitive", func(t *testing.T) {
		t.Parallel()
		c := serveCORS(mw, http.MethodGet, "https://app.example.com", nil)
		h := c.rec.Header()
		if !c.called || h.Get("Access-Control-Allow-Origin") != "https://app.example.com" ||
			h.Get("Access-Control-Expose-Headers") != "X-Trace, X-Other" ||
			h.Get("Access-Control-Allow-Credentials") != "true" || h.Get("Vary") != "Origin" {
			t.Fatalf("called=%v headers=%v", c.called, h)
		}
	})
	t.Run("wildcard origin", func(t *testing.T) {
		t.Parallel()
		c := serveCORS(mw, http.MethodPost, "https://img.cdn.example", nil)
		if c.rec.Header().Get("Access-Control-Allow-Origin") != "https://img.cdn.example" {
			t.Fatalf("wildcard did not match: %v", c.rec.Header())
		}
	})
	t.Run("origin not allowed: no headers, next still runs", func(t *testing.T) {
		t.Parallel()
		c := serveCORS(mw, http.MethodGet, "https://evil.example", nil)
		if !c.called || c.rec.Header().Get("Access-Control-Allow-Origin") != "" || c.rec.Header().Get("Vary") != "Origin" {
			t.Fatalf("called=%v headers=%v", c.called, c.rec.Header())
		}
	})
	t.Run("method not allowed (default GET/POST/HEAD): no headers, next still runs", func(t *testing.T) {
		t.Parallel()
		c := serveCORS(mw, http.MethodDelete, "https://app.example.com", nil)
		if !c.called || c.rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("called=%v headers=%v", c.called, c.rec.Header())
		}
	})
	t.Run("no Origin: only Vary", func(t *testing.T) {
		t.Parallel()
		c := serveCORS(mw, http.MethodGet, "", nil)
		if !c.called || len(c.rec.Header()) != 1 || c.rec.Header().Get("Vary") != "Origin" {
			t.Fatalf("called=%v headers=%v", c.called, c.rec.Header())
		}
	})
}

func TestCORS_AllowOriginFuncOverridesLists(t *testing.T) {
	t.Parallel()
	mw := CORS(CORSOptions{
		AllowedOrigins:  []string{"*"},
		AllowOriginFunc: func(_ *http.Request, origin string) bool { return strings.HasSuffix(origin, ".trusted") },
	})
	if c := serveCORS(mw, http.MethodGet, "https://a.trusted", nil); c.rec.Header().Get("Access-Control-Allow-Origin") != "https://a.trusted" {
		t.Fatalf("func-allowed origin must be echoed, not *: %v", c.rec.Header())
	}
	if c := serveCORS(mw, http.MethodGet, "https://a.other", nil); c.rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("func must win over the * list: %v", c.rec.Header())
	}
}

func TestCORS_WildcardHeadersAndOriginAlwaysAllowed(t *testing.T) {
	t.Parallel()
	anyHeader := CORS(CORSOptions{AllowedHeaders: []string{"*"}})
	if c := serveCORS(anyHeader, http.MethodOptions, "https://a.example", preflight("GET", "X-Anything, X-Else")); c.rec.Header().Get("Access-Control-Allow-Headers") != "X-Anything, X-Else" {
		t.Fatalf("* must allow any requested header: %v", c.rec.Header())
	}
	none := CORS(CORSOptions{})
	if c := serveCORS(none, http.MethodOptions, "https://a.example", preflight("GET", "origin")); c.rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("Origin header is implicitly allowed: %v", c.rec.Header())
	}
}

func TestCORS_DebugLogsCarryReason(t *testing.T) {
	t.Parallel()
	log, buf := newTestLogger()
	// logging.New defaults to Info; the debug lines must simply not break.
	mw := CORS(CORSOptions{AllowedOrigins: []string{"https://ok.example"}, Logger: log})
	serveCORS(mw, http.MethodGet, "https://evil.example", nil)
	if buf.Len() != 0 {
		t.Fatalf("debug lines must be filtered at Info: %s", buf.String())
	}
	log2 := logging.New(buf, logging.WithLevel(-4))
	mw = CORS(CORSOptions{AllowedOrigins: []string{"https://ok.example"}, Logger: log2})
	serveCORS(mw, http.MethodGet, "https://evil.example", nil)
	line := lastLine(t, buf)
	if line["message"] != "cors headers skipped" || line["cors.reason"] != "origin not allowed" || line["cors.origin"] != "https://evil.example" {
		t.Fatalf("log line = %v", line)
	}
}

// B1: wildcard origin plus credentials is rejected by every browser; refuse it
// at construction like CSRFProtect does with malformed origins.
func TestCORS_PanicsOnWildcardOriginWithCredentials(t *testing.T) {
	t.Parallel()
	for name, opts := range map[string]CORSOptions{
		"explicit *":   {AllowedOrigins: []string{"*"}, AllowCredentials: true},
		"empty = all":  {AllowCredentials: true},
		"mixed with *": {AllowedOrigins: []string{"https://a.example", "*"}, AllowCredentials: true},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: CORS must panic on * + credentials", name)
				}
			}()
			_ = CORS(opts)
		}()
	}
	// Explicit origins or a function with credentials are fine.
	_ = CORS(CORSOptions{AllowedOrigins: []string{"https://a.example"}, AllowCredentials: true})
	_ = CORS(CORSOptions{AllowOriginFunc: func(*http.Request, string) bool { return true }, AllowCredentials: true})
}

func TestAllowAll_DoesNotEnableCredentials(t *testing.T) {
	t.Parallel()
	c := serveCORS(AllowAll(), http.MethodDelete, "https://any.example", nil)
	h := c.rec.Header()
	if !c.called || h.Get("Access-Control-Allow-Origin") != "*" || h.Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("called=%v headers=%v", c.called, h)
	}
	c = serveCORS(AllowAll(), http.MethodOptions, "https://any.example", preflight("PATCH", "x-a"))
	if h := c.rec.Header(); h.Get("Access-Control-Allow-Methods") != "PATCH" || h.Get("Access-Control-Max-Age") != "43200" || h.Get("Access-Control-Allow-Headers") != "x-a" {
		t.Fatalf("preflight headers = %v", h)
	}
	// Opt-in override still works.
	c = serveCORS(AllowAll(func(o *CORSOptions) { o.MaxAge = 1 }), http.MethodOptions, "https://any.example", preflight("GET", ""))
	if c.rec.Header().Get("Access-Control-Max-Age") != "1" {
		t.Fatalf("override ignored: %v", c.rec.Header())
	}
}
