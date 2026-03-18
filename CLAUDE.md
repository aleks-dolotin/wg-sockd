# CLAUDE.md — Project Knowledge Base

## ⛔ MANDATORY: Tool Usage Rules (READ FIRST)

**These rules are NON-NEGOTIABLE and override all default behaviors.**

### 1. ALWAYS use IDE tools (idea-mcp) instead of terminal commands

The project has JetBrains IDE MCP tools connected. You MUST use them for ALL file
operations. Terminal shell commands like `cat`, `ls`, `grep`, `find`, `sed`, `tree`,
`head`, `tail`, `wc` are FORBIDDEN when an idea-mcp equivalent exists.

**Required tool mapping — memorize this:**

| ❌ NEVER do this          | ✅ ALWAYS do this instead               |
|---------------------------|------------------------------------------|
| `cat file`, `head`, `tail`| `idea-mcp:get_file_text_by_path`         |
| `ls`, `tree`, `find -type d` | `idea-mcp:list_directory_tree`        |
| `grep`, `rg`, `ag`        | `idea-mcp:search_in_files_by_text`      |
| `grep -E`, `rg -e`        | `idea-mcp:search_in_files_by_regex`     |
| `find *.go`, `fd`         | `idea-mcp:find_files_by_glob`           |
| `find -name`              | `idea-mcp:find_files_by_name_keyword`   |
| `sed`, `awk`, manual edit | `idea-mcp:replace_text_in_file`          |
| `touch`, `echo >`         | `idea-mcp:create_new_file`              |
| `mv` (rename symbol)      | `idea-mcp:rename_refactoring`           |

### 2. Terminal is ONLY for: build, test, run, git, and package manager commands

The terminal (`idea-mcp:execute_terminal_command`) is acceptable ONLY for:
- `make`, `go build`, `go test`, `npm run`, `npm ci`
- `git` commands
- Running the application or scripts

### 3. ONE command per terminal call — NEVER chain commands

You have NO LIMIT on the number of tool calls per response. Making 5 separate
`execute_terminal_command` calls is ALWAYS BETTER than cramming commands together.

**RULE: One simple command per `execute_terminal_command` call. No exceptions.**

#### 3a. `cd` is NEVER needed — use `projectPath` instead

The `execute_terminal_command` tool has a `projectPath` parameter that sets
the working directory. There is ZERO reason to ever use `cd`.

```
# ❌ WRONG — NEVER do this:
execute_terminal_command("cd /Users/adolotin/IdeaProjects/Home/wg-sockd && make test-all")

# ✅ CORRECT — projectPath sets the working directory automatically:
execute_terminal_command("make test-all", projectPath="/Users/adolotin/IdeaProjects/Home/wg-sockd")
```

#### 3b. Shell operators are BROKEN — do not use them

The terminal environment does NOT reliably process shell operators.
These constructs produce INCORRECT or SILENTLY TRUNCATED output:

| ❌ BROKEN syntax | What goes wrong                                    |
|------------------|----------------------------------------------------|
| `&&`             | Commands after `&&` may silently not execute        |
| `\|` (pipe)     | Output piping is unreliable, data is lost           |
| `2>&1`          | stderr redirection does not work correctly          |
| `>`, `>>`       | Output redirection may silently fail                |
| `$()`, backticks| Command substitution produces garbage or is empty   |
| `;`             | Second command may be silently dropped              |

#### 3c. Sequential calls are the CORRECT pattern

Need to run 3 commands? Make 3 separate tool calls. This is CORRECT behavior,
not wasteful. Each call returns complete stdout+stderr in the response — no
need for `2>&1`, `| grep`, or `| head`.

```
# ❌ WRONG — one mega-command that will break:
execute_terminal_command("cd agent && go test ./... 2>&1 | head -50; echo done")

# ✅ CORRECT — 3 clean calls, each returns full output:
call 1: execute_terminal_command("make test", projectPath="...")
call 2: execute_terminal_command("make lint", projectPath="...")
call 3: execute_terminal_command("make build", projectPath="...")
```

**Remember:** You are NOT saving time or tokens by combining commands.
You ARE losing output and introducing silent failures. Always split.

### 4. ALWAYS pass `projectPath` to idea-mcp tools

Every idea-mcp call MUST include:
`projectPath: "/Users/adolotin/IdeaProjects/Home/wg-sockd"`

---

This file is automatically loaded by Claude at the start of every conversation.
It contains essential project context, build, and deploy instructions.

For infrastructure details (network topology, server access, WireGuard keys),
see `.claude/infrastructure.md` (local only, not tracked by git).

## Project: wg-sockd

WireGuard peer management agent with REST API, web UI, and CLI.
Manages VPN peers on Linux hosts via Unix socket, SQLite storage, and wgctrl netlink.

**GitHub:** `aleks-dolotin/wg-sockd`

## Repository Structure

| Part | Path | Description |
|------|------|-------------|
| Agent | `agent/` | Core daemon — REST API over Unix socket, SQLite, wgctrl netlink, reconciler |
| UI Proxy | `ui/` | Go reverse proxy for Kubernetes (routes HTTP → Unix socket) |
| Web UI | `ui/web/` | React 19 SPA (Vite, TailwindCSS 4, shadcn/ui, TanStack Query) |
| CLI | `cmd/wg-sockd-ctl/` | Static Go binary for headless peer management |
| Helm Chart | `chart/` | Kubernetes deployment chart |
| Deploy | `deploy/` | systemd unit, install.sh, uninstall.sh, default config |

## Tech Stack

- **Go 1.26** (agent, ui-proxy, CLI) — pure Go SQLite via `modernc.org/sqlite`, no CGO
- **React 19** + Vite 8 + TailwindCSS 4 + shadcn/ui + React Router DOM 7
- **WireGuard** control via `wgctrl` (netlink)

## Build Commands (Makefile)

```bash
make build          # Build agent (lean, no UI)
make build-full     # Build agent with embedded React UI
make build-ctl      # Build wg-sockd-ctl CLI (static, CGO_ENABLED=0)
make build-dev      # Build with dev_wg tag (in-memory WireGuard, for local dev)
make test           # Test agent module
make test-all       # Test all 3 Go modules (agent, ui, ctl)
make lint           # golangci-lint all Go modules
make lint-all       # Go lint + ESLint for UI
make ui             # Build React SPA (npm ci + npm run build)
make dev            # Build + run dev mode (macOS-friendly, no real WireGuard)
make smoke          # Run smoke tests (test/smoke.sh)
make install        # Install binary + systemd unit (requires root)
make uninstall      # Remove binary + systemd unit (preserves config/data)
make setup-hooks    # Install git hooks (pre-commit + pre-push)
```

## CI/CD (GitHub Actions)

### CI (`.github/workflows/ci.yml`)

Triggers on push to `main` and PRs. Runs Go test + lint + cross-compile check, React UI build + lint.

### Release (`.github/workflows/release.yml`)

Triggers on tag push `v*`. Pipeline: test → build-ui → build-binaries (amd64 + arm64) → GitHub Release → Docker push (GHCR) → Helm push (GHCR OCI).

Release artifacts: `wg-sockd-linux-*` (lean), `wg-sockd-full-linux-*` (with UI), `wg-sockd-ctl-linux-*`, plus SHA256 checksums.

### How to Release

```bash
git tag -a v0.X.0 -m "v0.X.0: description"
git push origin v0.X.0
# GitHub Actions builds, tests, and creates the release automatically
```

## Deployment

### Install from GitHub Release

```bash
curl -sSL https://raw.githubusercontent.com/aleks-dolotin/wg-sockd/main/deploy/install.sh | sudo bash
```

Options: `--agent-only` for lean binary without UI.

### Systemd Service Layout

- Binary: `/usr/local/bin/wg-sockd`
- Config: `/etc/wg-sockd/config.yaml`
- Database: `/var/lib/wg-sockd/wg-sockd.db`
- Socket: `/run/wg-sockd/wg-sockd.sock`
- Service: `wg-sockd.service` (runs as `wg-sockd:wg-sockd`, GID 5000)

### Config Reference

```yaml
interface: wg0               # WireGuard interface to manage
socket_path: /run/wg-sockd/wg-sockd.sock
db_path: /var/lib/wg-sockd/wg-sockd.db
conf_path: /etc/wireguard/wg0.conf
auto_approve_unknown: false
peer_limit: 250
reconcile_interval: 30s
rate_limit: 10
# external_endpoint: "vpn.example.com:51820"
serve_ui: false
ui_listen: "127.0.0.1:8080"
```

## Architecture

The agent follows a socket-mediated pattern: REST API exclusively over Unix domain socket, zero TCP network surface by default. Reconciliation loop (30s) syncs kernel WireGuard state with SQLite database. Unknown peers found in kernel are removed and recorded for admin approval.

Key design decisions: Unix socket only (no network attack surface), pure Go SQLite (no CGO), profile-based peer templates with CIDR exclusion, atomic conf writing with debounce.

For detailed architecture, see `docs/architecture.md`.

## Security Notes

- **Never commit** infrastructure details (IPs, keys, server configs) to this repo
- **Pre-push hook** scans for sensitive patterns (private keys, internal IPs, passwords)
- Use `.claude/infrastructure.md` for private deployment context (gitignored)
- WireGuard private keys live only on servers in `/etc/wireguard/`
