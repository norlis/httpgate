package presenters

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

var log, _ = zap.NewDevelopment()

func TestNewPresentersJson(t *testing.T) {
	p := NewPresenters(log)

	handler := func(w http.ResponseWriter, r *http.Request) {
		p.JSON(w, r, map[string]string{"status": "success"})
	}

	req := httptest.NewRequest("GET", "http://example.com/foo", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	var d map[string]string
	err := json.Unmarshal(body, &d)

	if err != nil {
		t.Errorf(`NewPresenters.JSON result was incorrect, got %+v , want nil`, err)
	}

	if d["status"] != "success" {
		t.Errorf(`status = "%s", want success`, d["status"])
	}

	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf(`resp.Header.Get("Content-Type") = "%s", want application/json`, resp.Header.Get("Content-Type"))
	}
}

func TestNewPresentersPlainText(t *testing.T) {
	p := NewPresenters(log)

	handler := func(w http.ResponseWriter, r *http.Request) {
		p.PlainText(w, r, "hola PlainText")
	}

	req := httptest.NewRequest("GET", "http://example.com/foo", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	result := string(body)

	if result != "hola PlainText" {
		t.Errorf(`body = "%s", want "hola PlainText"`, result)
	}

	if resp.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf(`resp.Header.Get("Content-Type") = "%s", want text/plain; charset=utf-8`, resp.Header.Get("Content-Type"))
	}
}
