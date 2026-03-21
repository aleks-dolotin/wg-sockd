# wg-sockd — Release Process

## Overview

Releases are triggered by pushing a git tag (`v*`) to GitHub. The `release.yml` workflow handles everything: test → build → GitHub Release → Docker push → Helm push.

This document describes the manual steps before and after the automated pipeline.

## Prerequisites

- All changes committed and pushed to `main`
- All tests passing (`make test-all`)
- `CHANGELOG.md` updated with the new version section

## Step-by-Step

### 1. Update CHANGELOG

Add a new section at the top of `CHANGELOG.md`:

```markdown
## [v0.X.0] — YYYY-MM-DD

### Added
- ...

### Changed
- ...

### Fixed
- ...
```

If there are breaking changes, also update `UPGRADING.md`.

### 2. Bump Version

```bash
# Auto-increment minor version (reads current from VERSION file):
make bump-version

# Or specify exact version:
make bump-version VERSION_NEW=0.17.0
```

This updates:
- `VERSION` file
- `chart/Chart.yaml` (version + appVersion)
- `chart/values.yaml` (image tag)
- `docs/deployment-guide.md` (install commands)
- `README.md` (install commands)

### 3. Review and Commit

```bash
git diff
git add -A
git commit -m "release: v0.17.0"
```

### 4. Create Tag

```bash
git tag "v$(cat VERSION)"
```

### 5. Push to Main with Tag

Single push triggers CI and then release pipeline.

```bash
git push origin main --tags
```

> **Safety note:** Tag push triggers the full release pipeline (build, publish, deploy). This is intentionally a manual step that requires terminal access and cannot be executed by automated agents.

### 6. Verify Release

After the tag is pushed, GitHub Actions runs `release.yml`:

1. **Test** — all Go modules + UI lint
2. **Build UI** — React production build
3. **Build Binaries** — lean + full + CLI for amd64 and arm64
4. **GitHub Release** — creates release with binaries and checksums
5. **Docker Push** — multi-platform image to `ghcr.io/aleks-dolotin/wg-sockd-ui`
6. **Helm Push** — chart to `oci://ghcr.io/aleks-dolotin/charts`

Monitor at: `https://github.com/aleks-dolotin/wg-sockd/actions`

### 7. Deploy to Servers

#### Standalone (systemd)

```bash
# On each server (ssh home, ssh nas):
curl -sSL https://raw.githubusercontent.com/aleks-dolotin/wg-sockd/main/deploy/install.sh | sudo bash
sudo systemctl restart wg-sockd
```

Or for agent-only mode (K8s node):

```bash
curl -sSL https://raw.githubusercontent.com/aleks-dolotin/wg-sockd/main/deploy/install.sh | sudo bash -s -- --agent-only
```

#### Kubernetes (Helm)

```bash
helm upgrade wg-sockd-ui oci://ghcr.io/aleks-dolotin/charts/wg-sockd-ui \
  --version $(cat VERSION) \
  --namespace wg-sockd \
  --reuse-values
```

## Quick Reference

```
CHANGELOG.md  →  make bump-version  →  git commit  →  git tag  →  git push origin main --tags  →  (pipeline)  →  deploy
```
