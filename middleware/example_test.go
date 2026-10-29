package middleware_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/norlis/httpgate/logging"
	"github.com/norlis/httpgate/middleware"
	"github.com/norlis/httpgate/presenter"
)

// The README Quick Start: a JSON endpoint behind trace context, access
// logging and panic recovery. A service passes the handler to server.Run.
func Example() {
	logger := logging.New(
		io.Discard, // os.Stdout in a service
		logging.WithService("hello-service", "1.0.0"),
		logging.WithEnvironment("production"),
	)

	chain := middleware.New(
		middleware.TraceContext(),
		middleware.RequestLogger(logger),
		middleware.Recover(logger),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /hello", func(w http.ResponseWriter, r *http.Request) {
		presenter.JSON(w, r, map[string]string{"hi": "there"})
	})
	handler := chain.Then(mux)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/hello", http.NoBody))
	fmt.Println(rec.Code, rec.Body.String())

	// Output:
	// 200 {"hi":"there"}
}
