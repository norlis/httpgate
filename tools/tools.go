//go:build tools

// Package tools pins versions of development tools so go.mod tracks them.
// The build tag prevents these imports from ending up in the main binary.
package tools

import (
	// Linters y formateadores
	_ "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"
	_ "golang.org/x/tools/cmd/goimports"
	_ "mvdan.cc/gofumpt"
	_ "mvdan.cc/sh/v3/cmd/shfmt"

	_ "golang.org/x/vuln/cmd/govulncheck"
	// --- Análisis y Seguridad ---
	_ "honnef.co/go/tools/cmd/staticcheck"

	// Herramientas de desarrollo y testing
	_ "github.com/go-delve/delve/cmd/dlv"
	_ "golang.org/x/tools/gopls"
	_ "gotest.tools/gotestsum"
)
