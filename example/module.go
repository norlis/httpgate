package main

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/fx"

	"net/http"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func NewLogger() (*zap.Logger, error) {
	config := zap.NewProductionConfig()

	encoderConfig := zap.NewProductionEncoderConfig()

	encoderConfig.EncodeDuration = zapcore.StringDurationEncoder // Cambiamos el formato de la duración.
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder        // Formato de tiempo legible.
	encoderConfig.TimeKey = "timestamp"

	config.EncoderConfig = encoderConfig
	config.DisableStacktrace = true // Deshabilitar para no ser tan verboso, habilitar si se necesita para debug.

	if strings.EqualFold(os.Getenv("DEBUG"), "true") {
		config.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
		config.DisableStacktrace = false
	}

	return config.Build()
}

func NewHttpServerMux(lc fx.Lifecycle, logger *zap.Logger) *http.ServeMux {
	mux := http.NewServeMux()
	listener := ":8881"
	server := &http.Server{
		Addr:              listener,
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			logger.Info("Iniciando servidor HTTP")
			go func() {
				if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					logger.Error("Error al iniciar servidor HTTP: %v", zap.Error(err))
				}
			}()
			logger.Info(fmt.Sprintf("Servidor HTTP escuchando en %s", listener))
			return nil
		},
		OnStop: func(ctx context.Context) error {
			logger.Info("Deteniendo servidor HTTP...")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := server.Shutdown(shutdownCtx); err != nil {
				logger.Error("Error durante el apagado del servidor HTTP: %v", zap.Error(err))
				return err
			}
			logger.Info("Servidor HTTP detenido correctamente.")
			return nil
		},
	})

	return mux
}
