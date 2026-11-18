# FluxPanel

<p align="center">
  <a href="https://github.com/TingChieh/FluxPanel/blob/main/LICENSE"><img src="https://img.shields.io/github/license/TingChieh/FluxPanel" alt="License: GPL-3.0" /></a>
  <a href="https://github.com/TingChieh/FluxPanel/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/TingChieh/FluxPanel/ci.yml?branch=main" alt="CI" /></a>
  <a href="https://github.com/TingChieh/FluxPanel/releases"><img src="https://img.shields.io/github/v/release/TingChieh/FluxPanel" alt="Release" /></a>
</p>

**FluxPanel** is a lightweight, self-hosted multi-node control panel and subscription manager for [Xray-core](https://github.com/XTLS/Xray-core), forked from [MHSanaei/3x-ui](https://github.com/MHSanaei/3x-ui) under GPL-3.0.

[简体中文](README.zh_CN.md)

## Features

- **Multi-Protocol Xray Management**: VLESS (REALITY / Vision / XHTTP / gRPC), VMess, Trojan, Shadowsocks (including SS-2022), WireGuard, SOCKS, HTTP, plus native Go TUIC v5 / AmneziaWG and `mtg-multi` MTProto support.
- **Central Master & Remote Sub-Nodes**: Manage distributed nodes over HTTPS / mTLS (`verify`, `skip`, `pin`, `mtls`) with per-node client traffic breakdown and consolidated accounting.
- **Subscription Generator**: Outputs raw links, Xray JSON, and Clash / Mihomo YAML (including SIP002 Shadowrocket-friendly formatting and `dialer-proxy` relay templates).
- **Zero Sponsor / Ad Clutter**: Clean sidebar and panel UX focused strictly on operations and observability.
- **Drop-In Runtime Compatibility**: Preserves the `/etc/x-ui/x-ui.db` database layout, systemd service conventions, and `x-ui` CLI flags for painless upgrades from existing deployments.
- **Modern Stack**: Go backend (`Gin`, `GORM`, `Xray gRPC API`) + embedded React 19 / Ant Design 6 / TypeScript SPA with Zod 4 & OpenAPI schema codegen.

## Quick Install (Linux)

```bash
bash <(curl -Ls https://raw.githubusercontent.com/TingChieh/FluxPanel/main/install.sh)
```

Or run via Docker Compose using [`docker-compose.yml`](docker-compose.yml) / [`Dockerfile`](Dockerfile).

## Development

### Prerequisites

- Go `>= 1.27` (`CGO_ENABLED=1` required for `mattn/go-sqlite3`)
- Node.js `>= 26` and npm `>= 11`

### Common Commands

```bash
# Regenerate Zod + OpenAPI schemas from Go structs and check freshness
make gen-check

# Frontend checks & build (bundles into internal/web/dist/ for go:embed)
cd frontend && npm run typecheck && npm run lint && npm run format:check && npm test
make build-fe

# Backend checks & tests
go vet ./...
make test-go

# Cross-compile static Linux amd64 binary on macOS (example with zig)
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 CC="zig cc -target x86_64-linux-musl" \
  go build -trimpath -ldflags "-s -w -linkmode external -extldflags '-static'" -o x-ui .
```

See [CLAUDE.md](CLAUDE.md), [CONTRIBUTING.md](CONTRIBUTING.md), and [docs/architecture.md](docs/architecture.md) for codebase architecture and contributor guidelines.

## License & Attribution

FluxPanel is licensed under the [GNU General Public License v3.0](LICENSE).
Based on upstream [MHSanaei/3x-ui](https://github.com/MHSanaei/3x-ui) and powered by [XTLS/Xray-core](https://github.com/XTLS/Xray-core).
