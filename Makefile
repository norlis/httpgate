# --- Config ---
APP_IMPORT_PATH := $(shell go list -m)
ALL_PKGS := $(sort $(shell go list ./...))

TOOLS_BIN_DIR := $(abspath ./bin)
TOOLS_MOD_DIR := $(abspath ./tools)
export PATH := $(TOOLS_BIN_DIR):$(PATH)

.PHONY: help all test lint vulncheck format check-format tools tools-force mod-tidy modernize

help:
	@echo "Targets:"
	@echo "  tools          Install/refresh dev tools into ./bin from tools/go.mod"
	@echo "  tools-force    Wipe ./bin and reinstall"
	@echo "  lint           golangci-lint run --fix"
	@echo "  vulncheck      govulncheck ./..."
	@echo "  format         gofumpt -l -w ."
	@echo "  check-format   gofumpt -l . (CI mode)"
	@echo "  test           go test ./... --cover"
	@echo "  modernize      go fix -diff ./..."
	@echo "  mod-tidy       go mod tidy"

all: check-format lint test

# --- Tools ---
tools: $(TOOLS_BIN_DIR)

$(TOOLS_BIN_DIR): $(TOOLS_MOD_DIR)/tools.go $(TOOLS_MOD_DIR)/go.mod
	@echo "==> Installing tools from tools/go.mod..."
	@mkdir -p $(TOOLS_BIN_DIR)
	@cd $(TOOLS_MOD_DIR) && go mod tidy
	@cd $(TOOLS_MOD_DIR) && \
		go list -e -f '{{range .Imports}}{{.}} {{end}}' -tags=tools tools.go | \
		xargs -n1 env GOBIN=$(TOOLS_BIN_DIR) go install -v
	@touch $(TOOLS_BIN_DIR)
	@echo "==> Tools installed."

tools-force:
	@rm -rf $(TOOLS_BIN_DIR)
	@$(MAKE) tools

# --- Quality ---
lint: tools
	@$(TOOLS_BIN_DIR)/golangci-lint run --fix

vulncheck: tools
	@$(TOOLS_BIN_DIR)/govulncheck ./...

format: tools
	@$(TOOLS_BIN_DIR)/gofumpt -l -w .

check-format: tools
	@if [ -n "$$($(TOOLS_BIN_DIR)/gofumpt -l .)" ]; then \
		echo "ERROR: code not formatted with gofumpt:"; \
		$(TOOLS_BIN_DIR)/gofumpt -l .; \
		exit 1; \
	fi

# --- Tests ---
test:
	@go test ./... --cover

# --- Modernization & modules ---
modernize:
	@go fix -diff ./...

mod-tidy:
	@go mod tidy
