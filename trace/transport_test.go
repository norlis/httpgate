package trace

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTransport_InjectsTraceparent(t *testing.T) {
	t.Parallel()
	tc := Context{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7"}

	var got string
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(Header)
	}))
	defer upstream.Close()

	client := &http.Client{Transport: &Transport{}}
	ctx := NewContext(context.Background(), tc)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	if want := tc.Traceparent(); got != want {
		t.Fatalf("upstream received traceparent %q, want %q", got, want)
	}
}

func TestTransport_NoContextIsNoOp(t *testing.T) {
	t.Parallel()
	var got string
	var present bool
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(Header)
		_, present = r.Header[http.CanonicalHeaderKey(Header)]
	}))
	defer upstream.Close()

	client := &http.Client{Transport: &Transport{}}
	res, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	if present || got != "" {
		t.Fatalf("request without trace context must not carry traceparent, got %q", got)
	}
}

func TestTransport_DoesNotMutateOriginalRequest(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer upstream.Close()

	ctx := NewContext(context.Background(), New())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := (&http.Client{Transport: &Transport{}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	if req.Header.Get(Header) != "" {
		t.Fatal("RoundTrip must not mutate the caller's request")
	}
}
