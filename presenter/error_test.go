package presenter

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestError_defaults500(t *testing.T) {
	t.Parallel()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", http.NoBody)
	Error(rr, req, errors.New("boom"))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
	var pd map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&pd); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if pd["status"].(float64) != http.StatusInternalServerError {
		t.Fatalf("payload status = %v, want 500", pd["status"])
	}
	if pd["detail"] != "boom" {
		t.Fatalf("detail = %v, want boom", pd["detail"])
	}
}

func TestError_appliesStatusAndTitle(t *testing.T) {
	t.Parallel()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", http.NoBody)
	Error(
		rr, req, errors.New("bad input"),
		WithStatus(http.StatusBadRequest),
		WithTitle("validation failed"),
	)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	var pd map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&pd); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if pd["title"] != "validation failed" {
		t.Fatalf("title = %v, want validation failed", pd["title"])
	}
}
