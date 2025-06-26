package presenters

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestPresenters_Error(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	p := NewPresenters(logger)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.Error(w, r, errors.New("test"), WithStatus(http.StatusBadRequest))
	}))

	defer srv.Close()

	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/", srv.URL), nil)
	res, _ := http.DefaultClient.Do(req)

	bodyBytes, _ := io.ReadAll(res.Body)

	fmt.Printf("%+v", string(bodyBytes))
	if !strings.Contains(string(bodyBytes), `"title":"Bad Request","status":400,"detail":"test"`) {
		t.Errorf(`want contains "requestId":"12345", got %s`, string(bodyBytes))
	}
}
