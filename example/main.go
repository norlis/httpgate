package main

import (
	"context"
	"github.com/norlis/httpgate/pkg/health"
	"github.com/norlis/httpgate/pkg/middleware"
	"github.com/norlis/httpgate/pkg/opa"
	"go.uber.org/fx"
	"go.uber.org/zap"
	"log"
	"net/http"
)

func main() {
	app := fx.New(
		fx.Provide(NewLogger),
		fx.Provide(NewHttpServerMux),
		fx.Provide(func() *health.Status {
			return health.NewStatus("dev")
		}),
		fx.Invoke(func(router *http.ServeMux, status *health.Status, logger *zap.Logger) {

			opaConfig := opa.Config{
				Query:        "data.authz.allow",
				PoliciesPath: "policies/authz", // Directorio con authz.rego
				DataFiles:    []string{},
			}

			authz, err := opa.NewOpaSdkClientFromConfig(context.Background(), opaConfig, logger)

			if err != nil {
				log.Fatalf("No se pudo inicializar el cliente OPA: %v", err)
			}

			commons := []middleware.Middleware{
				middleware.Recover(logger),
				middleware.RequestLogger(logger),
				middleware.Cors(),
			}

			public := middleware.Chain(commons...)
			protected := middleware.Chain(
				append(
					commons,
					[]middleware.Middleware{middleware.AuthorizationMiddleware(authz)}...,
				)...,
			)

			//use := httpmiddleware.Chain(
			//	httpmiddleware.Recover(logger),
			//	httpmiddleware.RequestLogger(logger),
			//	httpmiddleware.Cors(),
			//	httpmiddleware.AuthorizationMiddleware(authz),
			//)
			base := http.NewServeMux()

			base.Handle("GET /status", status)
			base.Handle("GET /live", health.NewProbe(nil))
			base.Handle("GET /ready", health.NewProbe(nil)) // listo para aceptar trafico

			api := http.NewServeMux()
			api.HandleFunc("GET /test", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("api test"))
			})

			router.Handle("/", public(base))
			router.Handle("/api/", protected(http.StripPrefix("/api", api)))
			//router.Handle("/api/", use(api))
		}),
	)

	if err := app.Err(); err != nil {
		log.Panicf("Error en la inicialización de la aplicación FX: %v\n", err)
	}

	app.Run()
}
