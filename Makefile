.PHONY: build test install uninstall clean smoke

BINARY := wg-sockd
BIN_DIR := bin
INSTALL_DIR := /usr/local/bin
CONFIG_DIR := /etc/wg-sockd
SERVICE_FILE := /etc/systemd/system/wg-sockd.service

# Build the agent binary
build:
	cd agent && go build -o ../$(BIN_DIR)/$(BINARY) ./cmd/wg-sockd/
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
