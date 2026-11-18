# FluxPanel

<p align="center">
  <a href="https://github.com/TingChieh/FluxPanel/blob/main/LICENSE"><img src="https://img.shields.io/github/license/TingChieh/FluxPanel" alt="License: GPL-3.0" /></a>
  <a href="https://github.com/TingChieh/FluxPanel/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/TingChieh/FluxPanel/ci.yml?branch=main" alt="CI" /></a>
  <a href="https://github.com/TingChieh/FluxPanel/releases"><img src="https://img.shields.io/github/v/release/TingChieh/FluxPanel" alt="Release" /></a>
</p>

**FluxPanel** 是一个轻量级、多节点 [Xray-core](https://github.com/XTLS/Xray-core) 控制面板与订阅管理系统，基于 [MHSanaei/3x-ui](https://github.com/MHSanaei/3x-ui) 以 GPL-3.0 协议分支并重新整理出发。

[English](README.md)

## 核心特性

- **多协议 Xray 管理**：支持 VLESS（REALITY / Vision / XHTTP / gRPC）、VMess、Trojan、Shadowsocks（含 SS-2022）、WireGuard、SOCKS、HTTP，以及内置 Go TUIC v5 / AmneziaWG 和 `mtg-multi` MTProto。
- **主控 + 远程子节点架构**：通过 HTTPS / mTLS（`verify`、`skip`、`pin`、`mtls`）统一纳管远程节点，支持按节点拆分的客户端流量统计与汇总。
- **订阅生成与链式转发**：支持分享链接、Xray JSON 与 Clash / Mihomo YAML 输出（兼容 Shadowrocket SIP002 与 `dialer-proxy` 链式规则模板）。
- **无赞助广告干扰**：精简侧边栏与界面元素，专注于节点运维、日志排查与稳定性。
- **运行环境向下兼容**：保留 `/etc/x-ui/x-ui.db` 数据库路径、systemd 服务及 `x-ui` CLI 参数兼容，便于平滑迁移升级。
- **现代工程架构**：Go 后端（`Gin`、`GORM`、`Xray gRPC API`）+ 内嵌 React 19 / Ant Design 6 / TypeScript 前端，配合 Zod 4 与 OpenAPI Schema 自动校验生成。

## 快速安装 (Linux)

```bash
bash <(curl -Ls https://raw.githubusercontent.com/TingChieh/FluxPanel/main/install.sh)
```

或使用 [`docker-compose.yml`](docker-compose.yml) / [`Dockerfile`](Dockerfile) 部署容器版。

## 本地开发与校验

### 环境要求

- Go `>= 1.27`（`mattn/go-sqlite3` 依赖 `CGO_ENABLED=1` 与 C 编译器）
- Node.js `>= 26` 与 npm `>= 11`

### 常用命令

```bash
# 从 Go 结构体生成 Zod + OpenAPI Schema 并检查一致性
make gen-check

# 前端检查与构建（构建产物嵌入 internal/web/dist/）
cd frontend && npm run typecheck && npm run lint && npm run format:check && npm test
make build-fe

# 后端静态检查与测试
go vet ./...
make test-go

# macOS 交叉编译 Linux x86_64 静态二进制（需 zig）
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 CC="zig cc -target x86_64-linux-musl" \
  go build -trimpath -ldflags "-s -w -linkmode external -extldflags '-static'" -o x-ui .
```

更多源码结构与协作规范请参阅 [CLAUDE.md](CLAUDE.md)、[CONTRIBUTING.md](CONTRIBUTING.md) 与 [docs/architecture.md](docs/architecture.md)。

## 开源协议与致谢

本项目基于 [GNU General Public License v3.0](LICENSE) 发布。
致谢上游项目 [MHSanaei/3x-ui](https://github.com/MHSanaei/3x-ui) 与核心引擎 [XTLS/Xray-core](https://github.com/XTLS/Xray-core)。
