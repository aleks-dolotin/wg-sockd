# Changelog

All notable changes to this project will be documented in this file.

## [v0.13.0] — 2026-03-18

### Added

- **`client_address` field** — new peer field (CIDR, e.g. `10.0.0.2/32`) used as `[Interface] Address` in client download config. Validated at application level and enforced by partial unique DB index. Required for profile-based peers; /32 `AllowedIPs` used as fallback for legacy peers.
- **`last_seen_endpoint` field** — informational runtime endpoint updated by reconciler on every cycle (delta-only — only changed values written). Shown in API response and UI as read-only "Last Seen" field. Completely separate from `configured_endpoint` (not written to wg0.conf).
- **Approve onboarding** — `POST /api/peers/{id}/approve` expanded to full peer configuration: `friendly_name`, `profile`, `allowed_ips`, `client_address` (required), `configured_endpoint`, `client_dns`, `client_mtu`, `persistent_keepalive`. UI dialog shows `last_seen_endpoint` read-only with Copy button.
- **Disable/Enable peer toggle** — Enable/Disable button on peer list and edit page. Confirmation dialog before disable. Disabled peers shown grayed out.
- **PresharedKey full lifecycle** — `preshared_key` DB column with auto-generation via `wgtypes.GenerateKey()`. Triggered by profile flag `use_preshared_key: true` or explicit `preshared_key: "auto"` in API/CLI. Included in server wg0.conf, wgctrl kernel config, and client download conf. New PSK generated on key rotation.
- **PSK security** — PSK value never returned in `GET /api/peers` or `GET /api/peers/{id}`. Only `has_preshared_key: true/false` exposed. Full PSK returned one-time in create and rotate-keys responses (same pattern as PrivateKey).
- **`client_allowed_ips` split-tunnel** — new field on Peer and Profile with 4-level cascade (peer → profile → global → fallback `0.0.0.0/0, ::/0`). Replaces hardcoded full-tunnel in ClientConfBuilder. Configurable via `WG_SOCKD_CLIENT_ALLOWED_IPS` env var and `peer_defaults.client_allowed_ips` in config.yaml.
- **`use_preshared_key` profile flag** — when true, `CreatePeer` and `BatchCreatePeers` auto-generate a unique PSK per peer. Supported in `profiles create/update` CLI and UI.
- **SQLite migrations 005 and 006** — `client_address`, `last_seen_endpoint` (005); `preshared_key`, `client_allowed_ips` on peers and profiles (006). All new columns use empty-string defaults — backward compatible.
- **Startup warning** — agent logs WARN on start if any peers have empty `client_address` (client conf will fail for profile-based peers).
- **CLI `--client-address` flag** — added to `peers add`, `peers update`, `peers approve`. `--preshared-key` flag (auto/explicit) added to `peers add`. `--client-allowed-ips` flag added to `peers add/update` and `profiles create/update`.

### Fixed

- **Client config `[Interface] Address` bug** — was incorrectly set to server-side `AllowedIPs` (e.g. `10.0.0.0/8`) instead of the client's VPN IP. Fixed by `ResolveClientAddress()` using `client_address` field.
- **Reconciler endpoint pollution** — reconciler was writing runtime peer endpoint into `configured_endpoint` (wg0.conf), causing ephemeral roaming endpoints to persist. Runtime endpoint now stored only in `last_seen_endpoint`.
- **Zombie peer detection** — reconciler now explicitly removes disabled-in-DB peers that remain in kernel (access-control bypass). Previously only unknown peers were handled.

### Changed

- **`auto_approve_unknown` removed** — breaking change. All unknown peers require admin review via approve flow. If `auto_approve_unknown: true` found in config.yaml at startup, WARN logged "deprecated and ignored". Documented in UPGRADING.md.
- **Reconciler delta-only `last_seen_endpoint` updates** — collects all kernel endpoints, compares with DB, updates only changed peers in a single transaction. For stable networks = 0 writes per cycle.
- **PSK rollback safety in RotateKeys** — old PSK saved before rotation; restored in DB and wgctrl if any step fails.
- **Bump Helm chart version and appVersion to 0.13.0**
- **Bump image tag to 0.13.0**

### Breaking Changes

- `auto_approve_unknown` config field removed. Update `config.yaml` to remove this field before upgrading. See UPGRADING.md.

## [v0.6.0] — 2026-03-17

### Added

- **Dark mode** — Tailwind class-based toggle with localStorage persistence, no flash on reload. Sun/moon button in header.
- **Toast notifications** — sonner (native shadcn/ui) on all mutations: create, update, delete, approve, rotate keys. Success and error feedback.
- **Loading skeletons** — shadcn Skeleton composites on Dashboard, PeersPage, PeerDetailPage, ProfilesPage. Replaces "Loading..." text.
- **Error Boundary** — class-based React Error Boundary wrapping App with "Something went wrong" fallback + Reload button.
- **Stale data banner** — persistent amber warning when agent is disconnected: "Agent unavailable — data may be outdated".
- **PeerDetailPage full rewrite** — split into PeerStatusCard (online/offline/enabled badges, transfer RX/TX, endpoint, handshake), PeerActionsBar (enable/disable, rotate keys, download .conf, delete), PeerEditForm (name, profile, allowed IPs, notes). Graceful 404 when peer deleted externally.
- **Rotate keys dialog** — confirmation → API call → new QR code + download .conf + private key warning.
- **PeersPage search/filter/sort** — search by name or public key (300ms debounce), filter by status/profile/auto-discovered, sortable column headers. State persisted in URL via useSearchParams.
- **ProfilesPage delete confirmation** — dialog with API error display (e.g. "profile has peers assigned").
- **Dashboard top-20** — per-peer transfer bars limited to top 20 by traffic. Collapsible "Show all (N peers)" button.
- **Dynamic page titles** — usePageTitle hook sets document.title on each page (e.g. "Peers — wg-sockd").
- **Prometheus metrics endpoint** — GET /api/metrics with per-peer gauges/counters (rx/tx bytes, handshake, online, enabled) labeled by peer_name/public_key/profile + aggregate totals. Registered outside rate-limited router. Collector tests with mock wgctrl + in-memory DB.
- **Helm Prometheus annotations** — conditional pod annotations (prometheus.io/scrape, path, port) gated by prometheus.enabled value.
- **CLI global --json flag** — machine-readable JSON output on all commands for piping to jq.
- **CLI new commands** — peers get, peers update (--name/--profile/--allowed-ips/--notes/--enable/--disable), peers rotate-keys, profiles create, profiles update, profiles delete, health, stats. Full test coverage.
- **CLI --json retrofit** — existing peers list, peers add, peers delete, peers approve, profiles list all support --json.

### Fixed

- **UnknownPeerAlert link** — navigates to /peers?filter=auto_discovered (was /?filter=auto_discovered).
- **IPv6 CIDR validation** — isValidCIDR() now supports IPv6 addresses (fd00::1/64, ::/0, full addresses).
- **Dark mode CSS bug** — removed hardcoded body { bg-gray-50 text-gray-900 } that overrode @layer base dark mode styles.
- **index.html title** — changed from "web" to "wg-sockd".
- **Pre-existing ESLint warnings** — removed unused badgeVariants/buttonVariants exports from shadcn badge.jsx/button.jsx.

### Changed

- All UI components use dark-mode safe Tailwind classes (bg-background, text-foreground, bg-muted, etc.) instead of hardcoded gray-* colors.
- Layout.jsx header uses semantic color tokens.
- Bump Helm chart version and appVersion to 0.6.0
- Bump image tag to 0.6.0

## [v0.5.0] — 2026-03-16

### Fixed

- **All 50 golangci-lint issues resolved** — zero lint violations across all 3 Go modules (agent, ui, cmd/wg-sockd-ctl). Fixes 41 errcheck, 7 staticcheck (QF1012, SA4017, SA9003, QF1003), and 2 ineffassign issues that caused intermittent CI failures.
- **WireGuard conf permissions** — `install.sh` sets `/etc/wireguard/` to `0770 root:wg-sockd` and `.conf` files to `0660`. Private keys untouched.
- **`--dry-run` validates conf writability** — checks `conf_path` readability and parent directory writability. Prints actionable fix command on failure.
- **Health check `conf_writable` field** — `/api/health` reports `conf_writable` status. Degrades to `"degraded"` when agent cannot write to conf directory.

### Added

- **Pre-commit git hook** — `scripts/pre-commit` runs golangci-lint + go test on changed modules before each commit
- **Makefile targets** — `make lint` (all Go modules), `make lint-all` (Go + ESLint), `make setup-hooks` (install pre-commit hook)
- **MIT LICENSE file**
- **WireGuard permissions documentation** — deployment guide, README prerequisites, and security sections

### Changed

- Bump Helm chart version and appVersion to 0.5.0
- Bump image tag to 0.5.0

## [v0.4.0] — 2026-03-16

### Added

- **WireGuard permissions documentation** — `/etc/wireguard/` must be `0770 root:wg-sockd` for the agent to create peers. WireGuard defaults to `700 root:root` which blocks all write operations. Documented in deployment guide, README prerequisites, and security sections with comparison to wg-easy's root-in-Docker approach.
- **MIT LICENSE file** — proper license file in repo root, GitHub sidebar now shows MIT

### Fixed

- **`install.sh` sets `/etc/wireguard/` permissions** — `chown root:wg-sockd` + `chmod 770` on directory, `chmod 660` on `.conf` files. Private key files are not touched. Fresh installs now have a working write path out of the box.
- **`--dry-run` validates conf writability** — checks that `conf_path` is readable and its parent directory is writable (needed for atomic `tmp+rename`). Prints actionable fix command on failure.
- **Health check reports `conf_writable`** — new `conf_writable` field in `/api/health` response. Status degrades to `"degraded"` when the agent cannot write to the WireGuard config directory. Eliminates the false-positive `"status":"ok"` when write path is broken.

### Changed

- Bump Helm chart version and appVersion to 0.4.0
- Bump image tag to 0.4.0

### Known Issues

- **`wg-quick save` resets permissions** — `umask 077` reverts `wg0.conf` to `600 root:root`. Re-run `install.sh` or manually fix after `wg-quick save`.

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
