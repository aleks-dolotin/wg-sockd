# Changelog

All notable changes to this project will be documented in this file.

## [v0.20.0] — 2026-03-20

### Added

- **WebAuthn / Passkey Support** — implemented WebAuthn registration and login in both backend and frontend.
- **Passkey Management** — expanded `SettingsPage` to manage registered passkeys.
- **Passwordless Login** — added "Sign in with Passkey" button and conditional UI flow on login page.
- **Documentation** — updated `docs/authentication.md` with WebAuthn details.

### Changed

- **Login Flow** — merged password and passkey authentication flows.
- **Storage Schema** — added `webauthn_credentials` table.

## [v0.19.0] — 2026-03-20

### Added

- **Auto-fill Tunnel Address** — new API endpoint `GET /api/peers/next-address` computes the next free IP in the WireGuard interface subnet. The peer creation form auto-fills the Tunnel Address field on load.
- **Tunnel Address column in Peers table** — replaces Client Routing column. Default sort is now by tunnel address (numeric IP order) instead of name.
- **Dev mode documentation** — `docs/development-guide.md` now includes Dev Mode section with correct `WG_SOCKD_SOCKET` instructions.
- **Release process documentation** — `docs/release-process.md` describes the full release workflow.
- **Tests** — `TestNextAddress_InterfaceNotFound` (handler) and `TestListClientAddresses` (storage).

### Changed

- **PeersPage table** — removed "Client Routing" column, added "Tunnel Address" as first sortable column with numeric IP sorting.
- **Default sort order** — peers list now sorts by `tunnel_address` (ascending) instead of `name`.

### Fixed

- **PeerForm useEffect cleanup** — added `let ignore = false` pattern to prevent state update on unmounted component.
- **IPv6 status code** — `NextAddress` returns 501 (Not Implemented) instead of 500 for IPv6-only interfaces.
- **Safety cap for large subnets** — `NextAddress` scans at most 65535 addresses to avoid blocking on /8 subnets.

## [v0.16.0] — 2026-03-19

### Architecture Fix: Server AllowedIPs

**Breaking change** — database must be recreated from scratch (no migration path).

#### Fixed

- **Server `[Peer] AllowedIPs`** — now auto-derived as `/32` from `client_address` instead of using profile CIDR ranges. Fixes security issue where a client peer could spoof source IPs from entire subnets.
- **Kernel AllowedIPs** — peers now correctly get their tunnel IP as the only allowed source address in the WireGuard kernel.
- **Profile → Peer data flow** — profile `allowed_ips` (after CIDR math) now flows into `peer.client_allowed_ips` (client routing) instead of `peer.allowed_ips` (server ACL).

#### Changed

- **Database schema** — replaced incremental migration system with single `CREATE TABLE` schema. No `schema_version` table. Database is created from scratch on first start.
- **Profile `client_allowed_ips` removed** — profile `allowed_ips - exclude_ips` now directly determines client routing. Separate `client_allowed_ips` on profiles was redundant.
- **PeerForm UI** — reorganized into 4 sections: General, Tunnel Identity, Server-side, Client download config. Removed manual "Allowed IPs" input (server AllowedIPs is auto-derived). Updated all field labels and hints.
- **ProfileForm UI** — reorganized into 4 sections: General, Network Access (client routing), Server-side defaults, Client config defaults. "Allowed IPs" → "Allowed Networks", "Exclude IPs" → "Exclude Networks". Removed `client_allowed_ips` field.
- **PeerStatusCard** — shows "Tunnel Address" and "Client Routing" instead of "Profile" and "Allowed IPs".
- **PeersPage table** — "Allowed IPs" column → "Client Routing", shows `client_allowed_ips` instead of server AllowedIPs.

#### Removed

- `agent/internal/storage/migrations/` — entire migration system
- `cmd/migrate-cascade/` — one-time migration utility
- `profile.client_allowed_ips` — DB column, Go struct field, API field

#### Documentation

- `docs/field-architecture.md` — new canonical reference for field-to-WireGuard mapping
- `docs/gap-analysis.md` — gap analysis between old and new architecture

## [v0.15.0] — 2026-03-18

### Added

- **WYSIWYG peer configuration** — what you see in the UI is exactly what goes into the generated `.conf` file. No hidden lookups, no cascaded inheritance, no magic.
- **`GET /api/profiles/{name}`** — new API endpoint for fetching a single profile by name.
- **Profile create/edit pages** — full pages at `/settings/profiles/new` and `/settings/profiles/:name` replace modal dialogs.
- **`PeerForm` component** — unified form for peer create and edit with three sections (General, Server config, Client download config) and info tooltips.
- **`ProfileForm` component** — reusable form for profile create/edit with all profile fields.
- **`FieldLabel` component** — label with info tooltip for form fields.
- **CIDR validation** — strict `net.ParseCIDR` validation for `client_allowed_ips` in API handlers (create, batch, approve) and client-side validation in `PeerForm`.
- **CLI `--client-allowed-ips` required flag** — `wg-sockd-ctl peers add` now requires `--client-allowed-ips`.
- **Migration script** — `cmd/migrate-cascade/main.go` resolves existing cascade values into static DB records before upgrade.
- **SQLite migration 007** — drops `endpoint` column from profiles table.
- **Documentation** — `docs/profiles-and-cascade.md` WYSIWYG model reference.

### Changed

- **Configuration cascade removed** — the 4-level cascade (peer → profile → global → hardcoded) for generating client `.conf` files has been completely removed. Client config is now generated strictly from peer DB fields.
- **`peer_defaults` config section removed** — global defaults for DNS, MTU, PKA, ClientAllowedIPs no longer consulted. Set values on profiles or directly on peers.
- **`client_allowed_ips` and `client_address` now required** — `POST /api/peers` and `POST /api/peers/{id}/approve` return HTTP 400 without them.
- **`endpoint` removed from profiles** — the field was a design mistake (endpoint is unique per peer, not a shared profile default). Migration 007 drops the column.
- **PSK generation controlled by client request only** — profile's `use_preshared_key` flag pre-checks the UI checkbox but the backend respects only what the client sends.
- **Profile pre-fill model** — profiles are templates for pre-filling forms, not enforced policies. Changing a profile does not affect existing peers.
- **`resolved_*` fields removed from API** — `resolved_client_dns`, `resolved_client_mtu`, `resolved_client_persistent_keepalive`, `resolved_client_allowed_ips` and their `*_source` counterparts are gone.
- **Bump Helm chart version and appVersion to 0.15.0**

### Removed

- `PeerDefaultsConfig` struct and all related env vars (`WG_SOCKD_CLIENT_DNS`, `WG_SOCKD_CLIENT_MTU`, `WG_SOCKD_CLIENT_PERSISTENT_KEEPALIVE`, `WG_SOCKD_CLIENT_ALLOWED_IPS`).
- `ResolveClientConf` function and `ResolvedConf` types from `confwriter` package.
- `resolveClientConfForPeer` from API handlers.
- Profile `endpoint` field (DB column and API).

### Breaking Changes

- Configuration cascade removed. Run `cmd/migrate-cascade` before upgrading. See UPGRADING.md.
- `peer_defaults` config section ignored. Remove it from `config.yaml`.
- `client_allowed_ips` and `client_address` now required in create/approve API calls.
- `resolved_*` fields removed from `GET /api/peers` responses.
- Profile `endpoint` field removed.

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
