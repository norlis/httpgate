package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/norlis/httpgate/pkg/adapter/apidriven/middleware"
	"github.com/norlis/httpgate/pkg/adapter/apidriven/presenters"
	"github.com/norlis/httpgate/pkg/adapter/opa"
	"github.com/norlis/httpgate/pkg/application/health"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

// banner
// https://patorjk.com/software/taag/#p=display&f=DiamFont&t=EXAMPLE
const banner = `
▗▄▄▄▖▗▖  ▗▖ ▗▄▖ ▗▖  ▗▖▗▄▄▖ ▗▖   ▗▄▄▄▖
▐▌    ▝▚▞▘ ▐▌ ▐▌▐▛▚▞▜▌▐▌ ▐▌▐▌   ▐▌   
▐▛▀▀▘  ▐▌  ▐▛▀▜▌▐▌  ▐▌▐▛▀▘ ▐▌   ▐▛▀▀▘
▐▙▄▄▖▗▞▘▝▚▖▐▌ ▐▌▐▌  ▐▌▐▌   ▐▙▄▄▖▐▙▄▄▖
`

func main() {

	fmt.Print(banner)

	app := fx.New(
		fx.Provide(NewLogger),
		fx.Provide(NewHttpServerMux),
		fx.Provide(func() *health.Status {
			return health.NewStatus("dev")
		}),
		fx.Provide(presenters.NewPresenters),
		fx.Invoke(func(router *http.ServeMux, status *health.Status, logger *zap.Logger, render presenters.Presenters) {

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
				middleware.TraceId(middleware.WithHeaderName("X-Request-ID")),
				middleware.APIErrorMiddleware(
					middleware.WithIntercept(http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusInternalServerError),
					middleware.WithCustomMessage(http.StatusNotFound, "resource not found"),
					middleware.WithCustomMessage(http.StatusMethodNotAllowed, "method is not allowed for this resource."),
				),
				middleware.Recover(logger, render),
				middleware.RequestLogger(logger),
				middleware.AllowAll(logger).Middleware,
			}

			public := middleware.Chain(commons...)
			protected := middleware.Chain(
				append(
					commons,
					[]middleware.Middleware{middleware.AuthorizationMiddleware(
						authz,
						func(r *http.Request) (map[string]any, error) {
							return map[string]any{"roles": []string{}}, nil
						},
					)}...,
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
				render.JSON(
					w, r,
					map[string]string{"text": "Hello World"},
					presenters.WithStatusCode(http.StatusAccepted),
					presenters.WithHeader("x-test", "1"),
				)
			})

			api.HandleFunc("GET /test-err", func(w http.ResponseWriter, r *http.Request) {
				render.Error(w, r, errors.New("error ocurred"), presenters.WithStatus(http.StatusBadRequest))
			})

			api.HandleFunc("GET /panic", func(w http.ResponseWriter, r *http.Request) {
				panic(errors.New("panic test"))
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
