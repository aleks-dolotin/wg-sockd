# Changelog

All notable changes to this project will be documented in this file.

## [v0.4.0] — 2026-03-16

### Changed

- Bump version to 0.4.0 for full-flow validation
- Update Helm chart version and appVersion to 0.4.0
- Update image tag to 0.4.0

## [v0.3.0] — 2026-03-17

### Added

- **Dev mode** — in-memory WireGuard client (`--dev-wg` flag) behind `dev_wg` build tag; `make dev` / `make build-dev` for local development on macOS without real WireGuard
- **GET /api/peers/{id}** — single peer endpoint with live wgctrl data merge
- **Edit Peer page** — full edit form (name, profile, allowed IPs, notes) replacing stub
- **Unicode friendly names** — peer names now support Cyrillic, CJK, and all Unicode letters
- **Vite Unix socket proxy** — `WG_SOCKD_SOCKET` env var for dev mode, no TCP listener needed
- **SQL migration 003** — drops `display_name` column from profiles table

### Changed

- **Dashboard is now the landing page** (`/`) — Peers moved to `/peers`
- **Navigation order** — Dashboard → Peers → Profiles; removed Add Peer from top nav (button exists on Peers page)
- **Connection status** — uses `/api/health` instead of `/ui/status`; eliminates 404 console spam in dev mode and standalone
- **Add Peer form** — shows Allowed IPs field directly when no profiles exist (no need to select "Custom")
- **Profile model simplified** — removed `display_name` from entire stack (SQL, Go, API, CTL, UI, docs)
- **chart/values.yaml** — image repository now points to `ghcr.io/aleks-dolotin/wg-sockd-ui`

### Fixed

- **RuntimeDirectory removed** from systemd unit — prevents K8s hostPath inode mismatch on agent restart
- **install.sh** — creates `/run/wg-sockd/` with correct permissions; socket access hint with `$USER`
- **release.yml** — ldflags injected for all build targets; per-file `.sha256` checksums
- **CTL profiles list** — fixed format string mismatch after display_name removal

## [v0.2.0] — 2026-03-16

### Installation UX Overhaul

Major improvement to the installation and operational experience. 34 tasks, 15 files modified, 59 acceptance criteria.

#### Added

- **Version system** — `--version` flag for both `wg-sockd` and `wg-sockd-ctl` binaries, with build metadata injected via ldflags (commit, date, build tags)
- **`wg-sockd-ctl version` subcommand** — prints version info without connecting to socket
- **`--dry-run` flag** — validates config, ui_listen format, directory permissions, and WireGuard availability without starting services
- **4-level config** — default → YAML file → environment variables → CLI flags; new `ApplyEnv()` method supports `WG_SOCKD_*` env vars
- **Config-driven UI** — `serve_ui` and `ui_listen` fields in config.yaml control the embedded web UI
- **Config override logging** — non-default config values logged with source (yaml/env/cli)
- **`make dev`** — local development mode with isolated config in `./tmp/`, macOS degraded mode support
- **install.sh `--agent-only` flag** — installs lean binary without UI for K8s/headless deployments
- **install.sh interactive mode** — prompts for UI binding address when running in a terminal
- **install.sh SHA256 verification** — checksums verified on binary download when available
- **install.sh WireGuard detection** — checks for `wg` in PATH with distro-specific install hints (Ubuntu, Fedora, CentOS, Arch, openSUSE, Alpine + generic fallback)
- **install.sh upgrade path** — safely appends `serve_ui`/`ui_listen` to existing configs without duplicating
- **install.sh `--version` verification** — validates installed binary after download, warns if full-mode binary lacks `+ui` tag
- **Troubleshooting section** in deployment-guide.md
- **Prerequisites, Verifying Installation, Uninstall, Development sections** in README.md
- **smoke.sh** — `--version` and `--dry-run` integration checks

#### Changed

- **Makefile** — all build targets now inject version via LDFLAGS; `build-full` sets `+ui` build tag
- **Explicit boolean merge** — `serve_ui` from config can be overridden by CLI `--serve-ui`; `--serve-ui-dir` operates independently
- **Config/CLI source distinction** — `serve_ui: true` in config with lean build → warning; `--serve-ui` CLI with lean build → fatal
- **Bool validation alignment** — Go (`strconv.ParseBool`) and bash (`install.sh`) both accept `true/false/1/0/t/f` case-insensitive
- **README Quick Start** — rewritten for one-command install with automatic UI

#### Removed

- **kubectl label block** in install.sh — removed per spec (AC-31)
- **`RuntimeDirectory=wg-sockd`** in systemd unit — removed to keep directory inode stable for Kubernetes hostPath mounts. The agent creates `/run/wg-sockd/` on startup and cleans up stale sockets itself.

#### Security

- **0.0.0.0 binding warning** — bold security warning when UI is bound to all interfaces
- **Non-interactive install** — defaults to `0.0.0.0:8080` with prominent warning when piped

### New Tests

- `TestDefaults_ServeUIAndUIListen`, `TestLoadConfig_ServeUIAndUIListen`
- `TestApplyEnv_StringOverrides`, `TestApplyEnv_BoolOverride`, `TestApplyEnv_BoolParseBoolVariants`
- `TestApplyEnv_InvalidBool`, `TestApplyEnv_NoEnvVars`, `TestApplyEnv_MapKeyFormat`
- `TestVersionVarsHaveDefaults` (agent), `TestCTLVersionVarsHaveDefaults` (CTL)
- `TestDryRun_ValidConfig`, `TestDryRun_InvalidUIListen`, `TestDryRun_MissingDataDir`, `TestDryRun_NotWritableDataDir`
- `TestPrintVersion` (CTL)

## [v0.1.0] — 2026-03-15

Initial release.
