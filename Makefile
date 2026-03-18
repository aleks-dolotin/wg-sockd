.PHONY: build test install uninstall clean smoke docker-build test-ui build-ctl test-ctl build-full ui dev lint lint-all setup-hooks

BINARY := wg-sockd
BIN_DIR := bin
INSTALL_DIR := /usr/local/bin
CONFIG_DIR := /etc/wg-sockd
SERVICE_FILE := /etc/systemd/system/wg-sockd.service

# Version info injected at build time
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
AGENT_LDFLAGS := -ldflags="-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)"
AGENT_LDFLAGS_UI := -ldflags="-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE) -X main.buildTags=ui"
CTL_LDFLAGS := -ldflags="-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)"

# Build the agent binary
build:
	cd agent && go build $(AGENT_LDFLAGS) -o ../$(BIN_DIR)/$(BINARY) ./cmd/wg-sockd/
	@echo "Built $(BIN_DIR)/$(BINARY)"

# Run all tests
test:
	cd agent && go test ./...

# Run tests with verbose output
test-v:
	cd agent && go test -v ./...

# Install the agent (requires root)
install: build
	@echo "Installing wg-sockd..."
	# Create system user if not exists
	id -u wg-sockd >/dev/null 2>&1 || useradd -r -s /usr/sbin/nologin -d /nonexistent wg-sockd
	# Add wg-sockd user to wg-sockd group for socket access
	usermod -aG wg-sockd wg-sockd 2>/dev/null || true
	# Copy binary
	install -m 0755 $(BIN_DIR)/$(BINARY) $(INSTALL_DIR)/$(BINARY)
	# Install config
	mkdir -p $(CONFIG_DIR)
	[ -f $(CONFIG_DIR)/config.yaml ] || install -m 0640 deploy/config.yaml $(CONFIG_DIR)/config.yaml
	# Install systemd unit
	install -m 0644 deploy/wg-sockd.service $(SERVICE_FILE)
	systemctl daemon-reload
	systemctl enable wg-sockd
	@echo "Installed. Start with: systemctl start wg-sockd"

# Uninstall the agent (requires root)
uninstall:
	@echo "Uninstalling wg-sockd..."
	systemctl stop wg-sockd 2>/dev/null || true
	systemctl disable wg-sockd 2>/dev/null || true
	rm -f $(SERVICE_FILE)
	rm -f $(INSTALL_DIR)/$(BINARY)
	systemctl daemon-reload
	@echo "Uninstalled. Config and data preserved in $(CONFIG_DIR) and /var/lib/wg-sockd"

# Run smoke tests
smoke:
	bash test/smoke.sh

# Clean build artifacts
clean:
	rm -rf $(BIN_DIR)
	cd agent && rm -f wg-sockd

# Build React UI (for embedded mode)
ui:
	cd ui/web && npm ci && npm run build
	@echo "React UI built"

# Build agent with embedded UI (~30MB)
build-full: ui
	rm -rf agent/cmd/wg-sockd/ui_dist
	cp -r ui/web/dist agent/cmd/wg-sockd/ui_dist
	cd agent && go build -tags embed_ui $(AGENT_LDFLAGS_UI) -o ../$(BIN_DIR)/$(BINARY)-full ./cmd/wg-sockd/
	@echo "Built $(BIN_DIR)/$(BINARY)-full (with embedded UI)"

# Build UI Docker image
docker-build:
	docker build -t wg-sockd-ui:latest -f ui/Dockerfile ui/

# Run UI proxy tests
test-ui:
	cd ui && go test ./...

# Build wg-sockd-ctl CLI binary
build-ctl:
	cd cmd/wg-sockd-ctl && CGO_ENABLED=0 go build $(CTL_LDFLAGS) -o ../../$(BIN_DIR)/wg-sockd-ctl .
	@echo "Built $(BIN_DIR)/wg-sockd-ctl"

# Run CLI tests
test-ctl:
	cd cmd/wg-sockd-ctl && go test ./...

# Run all tests across all modules
test-all: test test-ui test-ctl

# Lint all Go modules with golangci-lint
lint:
	cd agent && golangci-lint run ./...
	cd ui && golangci-lint run ./...
	cd cmd/wg-sockd-ctl && golangci-lint run ./...

# Lint all (Go + ESLint)
lint-all: lint
	cd ui/web && npx eslint .

# Install git hooks (pre-commit + pre-push)
setup-hooks:
	@cp scripts/pre-commit .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@cp scripts/pre-push .git/hooks/pre-push
	@chmod +x .git/hooks/pre-push
	@echo "Git hooks installed (pre-commit + pre-push)"

# Local development mode — API-only, no WireGuard needed (macOS degraded OK).
# Uses ./tmp/ for isolated dev config and data.
# NOTE: Environment variables (WG_SOCKD_*) from your shell will still apply.
#       Use `env -u WG_SOCKD_INTERFACE ... make dev` to isolate fully.
dev: build-dev
	@mkdir -p ./tmp

# Build agent with dev_wg tag for local development (in-memory WireGuard)
build-dev:
	cd agent && go build -tags dev_wg $(AGENT_LDFLAGS) -o ../$(BIN_DIR)/$(BINARY) ./cmd/wg-sockd/
	@echo "Built $(BIN_DIR)/$(BINARY) (with dev_wg)"
	@mkdir -p ./tmp
	@chmod 700 ./tmp
	@if [ -f ./tmp/dev-config.yaml ]; then \
		echo "Using existing dev config (delete ./tmp/dev-config.yaml to regenerate)"; \
	else \
		echo "Generating dev config in ./tmp/dev-config.yaml"; \
		printf 'interface: wg0\nsocket_path: ./tmp/wg-sockd.sock\ndb_path: ./tmp/wg-sockd.db\nconf_path: ./tmp/wg0.conf\nauto_approve_unknown: false\npeer_limit: 250\nreconcile_interval: 30s\nrate_limit: 10\n' > ./tmp/dev-config.yaml; \
	fi
	@touch ./tmp/wg0.conf
	./$(BIN_DIR)/$(BINARY) --config ./tmp/dev-config.yaml --dev-wg

