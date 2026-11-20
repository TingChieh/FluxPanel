# 独立「流量统计」页面 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 FluxPanel 新增独立的「流量统计」页面（`/panel/traffic`，方案 B：首屏 KPI + 全站趋势图 + 用户/节点双榜并排 + 右侧详情抽屉），补齐单机与多节点按小时/按天持久化流量账本和统一实时速率追踪，支持日/周/月精细查询与下钻分析。

**Architecture:** 后端新增 `traffic_hourlies`（保留 90 天）和 `traffic_dailies`（归档保留 365 天）两张独立于配额重置的时序表，通过进程内 `TrafficLedger` 聚合 5 秒增量并每 60 秒经 `submitTrafficWriteBackground` 批量刷盘（查询时实时合并内存未刷盘增量）；另增 `LiveSpeedTracker` 汇总本机与远端节点的 5 秒实时速率并随 WebSocket `traffic` 帧广播。前端基于现有 Ant Design 6 + `uplot` 构建 `/panel/traffic` 页面与双榜/抽屉视图，用户列表及节点明细均走服务端分页/搜索/排序以支撑万级用户规模。

**Tech Stack:** Go 1.27, Gin, GORM (SQLite `mattn/go-sqlite3`), React 19, TypeScript, Ant Design 6, TanStack Query, `uplot`, `openapigen` + Zod + Fumadocs OpenAPI。

**Spec:** `docs/superpowers/specs/2026-11-20-traffic-analytics-page-design.md`

## Global Constraints

- 仅使用现有栈（Go/SQLite + React 19/Ant Design 6/`uplot`），不引入新 UI 框架或图表库。
- 全部日/周/月边界切分、归档与基准「今天」统一采用面板配置的 IANA `timeLocation`（缺省 `time.Local`）。
- 历史账本独立于配额重置与用户/节点删除；用户改名时历史流量合并迁移至新 `email`。
- 所有写库走 `submitTrafficWriteBackground` / 串行写事务，禁止在 5 秒热路径直接高频写 SQLite。
- 新增路由必须同步更新 `frontend/src/pages/api-docs/endpoints.ts`、`tools/openapigen/main.go`（`StructAllow` + `example:` tag）、`make gen`、`docs/public/openapi.json` 及 `cd docs && pnpm gen:api`。
- 新增英文/中文文案同步写入 `internal/web/translation/en-US.json` 和 `zh-CN.json`，且在同提交内引用（通过 `i18n-dead-keys.test.ts`）。
- 注释每块最多 2 行；遵循 TDD（先写失败测试再实现）与 Conventional Commits。

## Review Focus

1. **DST 23h/25h 切换日**：在支持夏令时的 IANA 时区（如 `America/New_York`）查询切换日的「日」视图时，必须按真实本地整点步进输出 23 或 25 个桶，不得硬编码 24×3600s 导致缺失或覆盖 — 锁定在 **Task 4** 测试。
2. **远端节点首次接入老数据**：首次同步无 baseline 的远端节点时，不得把节点原有历史累计流量记作当前小时突发增量，仅从建立 baseline 后的真实新增开始记账 — 锁定在 **Task 2** 测试。
3. **刷盘失败回滚与未刷盘实时读取**：`FlushTrafficLedger` 事务失败时必须将取出增量合并回内存缓冲防止丢数；查询接口必须实时合并 DB 与内存未刷盘数据且不重复累加 — 锁定在 **Task 1** 测试。
4. **跨 90 天归档边界的周/月查询**：当一周或一月横跨 `cutoffDate` 时，前段读 `traffic_dailies`、后段读 `traffic_hourlies`，边界日严格互斥零重复零漏算 — 锁定在 **Task 4** 测试。
5. **改名目标 `email` 在历史表已存在（曾被已删用户占用）**：`RenameTrafficHistoryTx` 必须使用 `ON CONFLICT DO UPDATE` 累加合并再删旧行，不能直接 `UPDATE` 触发 SQLite UNIQUE 冲突 — 锁定在 **Task 1** 与 **Task 2** 测试。

---

## File Map

- **Create (Go):**
  - `internal/database/model/traffic_history.go` — `TrafficHourly`, `TrafficDaily` GORM 模型与索引
  - `internal/web/service/traffic_ledger.go` — 内存累加器 `TrafficLedger`、刷盘、改名迁移、90/365 天归档清理
  - `internal/web/service/live_speed.go` — 跨本机与远端节点的 5 秒实时速率缓存与 15 秒衰减
  - `internal/web/service/traffic_analytics.go` — 日/周/月聚合查询、服务端分页排行、用户/节点抽屉明细与 OpenAPI 响应结构体
  - `internal/web/job/traffic_ledger_job.go` — `TrafficFlushJob`（60s）与 `TrafficRollupJob`（每日）
  - `internal/web/controller/traffic.go` — `/panel/api/traffic/{overview,clients,client/:email,node/:id}`
  - Tests: `traffic_ledger_test.go`, `traffic_collection_test.go`, `live_speed_test.go`, `traffic_analytics_test.go`, `controller/traffic_test.go`
- **Modify (Go):**
  - `internal/database/db.go` — `allModels()` 注册新表
  - `internal/web/service/inbound_traffic.go` — 本机 `addTrafficLocked` 提交后记账
  - `internal/web/service/inbound_node.go` — 远端 `SetRemoteTraffic` 提交后记账（含首次接入护栏）
  - `internal/web/service/client_crud.go` & `internal/web/service/client_inbound_apply.go` — 改名串行事务调用 `RenameTrafficHistoryTx`
  - `internal/web/job/xray_traffic_job.go` & `internal/web/job/node_traffic_sync_job.go` — 更新 `LiveSpeedTracker` 并扩展 WS `traffic` 载荷
  - `internal/web/web.go` — 注册定时任务与停机 `FlushTrafficLedger()`
  - `internal/web/controller/api.go` & `internal/web/controller/spa.go` — 挂载 `/panel/api/traffic` 与 `/panel/traffic`
  - `tools/openapigen/main.go` — 注册新增响应结构体到 `StructAllow`
- **Create (Frontend):**
  - `frontend/src/schemas/traffic.ts` — Zod 响应 schema（基于生成类型）与查询参数类型
  - `frontend/src/api/queries/useTrafficQueries.ts` — TanStack Query hooks（overview / clients / clientDetail / nodeDetail）
  - `frontend/src/hooks/useLiveTrafficSpeeds.ts` — 订阅 WS `traffic` 实时速率字段与连接状态
  - `frontend/src/components/viz/TrafficTrendChart.tsx` + `.css` — `uplot` 上下行双系列趋势图（支持未来桶与三套主题）
  - `frontend/src/pages/traffic/TrafficPage.tsx` + `.css` — 方案 B 主页面（顶部工具栏、4 KPI、趋势图、双榜并排）
  - `frontend/src/pages/traffic/TrafficDetailDrawer.tsx` — 用户抽屉 + 节点抽屉（含交叉跳转与节点内用户搜索分页）
  - `frontend/src/test/traffic-page.test.tsx` — 前端页面、实时刷新、筛选分页与抽屉交互测试
- **Modify (Frontend & Docs):**
  - `frontend/src/api/queryKeys.ts` — 新增 `keys.traffic.*`
  - `frontend/src/routes/panelRoutes.tsx` — 注册 `/traffic` 路由、菜单与命令面板入口
  - `frontend/src/pages/api-docs/endpoints.ts` — 注册 4 个 GET 端点
  - `internal/web/translation/en-US.json` & `zh-CN.json` — 新增流量统计页双语文案
  - Generated: `frontend/src/generated/*`, `frontend/public/openapi.json`, `docs/public/openapi.json`, `docs/content/docs/en/reference/api/*`

---

### Task 1: 历史账本模型与 `TrafficLedger`（缓冲、刷盘、改名合并、归档清理）

**Files:**
- Create: `internal/database/model/traffic_history.go`
- Modify: `internal/database/db.go:53-81`
- Create: `internal/web/service/traffic_ledger.go`
- Test: `internal/web/service/traffic_ledger_test.go`

**Interfaces:**
- Consumes: `database.GetDB()`, `submitTrafficWriteBackground` (`internal/web/service/traffic_writer.go`)
- Produces:
  - `model.TrafficHourly` (`Id`, `HourBucket`, `NodeId`, `Email`, `Up`, `Down`)
  - `model.TrafficDaily` (`Id`, `Date`, `NodeId`, `Email`, `Up`, `Down`)
  - `type TrafficLedgerKey struct { HourBucket int64; NodeId int; Email string }`
  - `type TrafficDelta struct { Up int64; Down int64 }`
  - `RecordTrafficDelta(at time.Time, nodeID int, email string, up, down int64)`
  - `SnapshotPendingTraffic() map[TrafficLedgerKey]TrafficDelta`
  - `FlushTrafficLedger() error`
  - `ResetTrafficLedgerForTest()`
  - `RenameTrafficHistoryTx(tx *gorm.DB, oldEmail, newEmail string) error`
  - `RollupAndPruneTrafficHistory(now time.Time, loc *time.Location) error`
  - `TrafficHourlyCutoffDate(now time.Time, loc *time.Location) (string, int64)`（返回 `YYYY-MM-DD` 与对应 `loc` 零点 Unix 秒，严格保留最近 90 个完整自然日）

- [ ] **Step 1: Write failing tests in `internal/web/service/traffic_ledger_test.go`**

覆盖 5 个核心场景（使用 `dbtest.InitDB`）：
1. `TestTrafficLedger_RecordSnapshotAndFlushUpsert`：同 `(hour, node, email)` 多次 `RecordTrafficDelta` 在 flush 前可通过 `SnapshotPendingTraffic` 读到总和；flush 写入 `traffic_hourlies` 后内存清空，再次 record 同键并 flush 正确累加（UPSERT）。
2. `TestTrafficLedger_FlushFailureRequeuesDeltas`：DB 关闭或表损坏时 `FlushTrafficLedger()` 返回 error 且未写入的 delta 完整回滚保留在 `SnapshotPendingTraffic()` 中不丢失。
3. `TestTrafficLedger_RenameMergesBufferAndTablesOnConflict`：`traffic_hourlies`、`traffic_dailies` 和未刷盘内存缓冲中同时存在 `oldEmail` 与已占用的 `newEmail` 行，调用 `RenameTrafficHistoryTx` 后三处均精准累加到 `newEmail` 且 `oldEmail` 行全部清除（Review Focus #5）。
4. `TestTrafficLedger_RollupAndPruneStrictCutoff`：构造 `cutoffDate` 前 1 天、当天、以及 366 天前的小时/按天记录，指定非 UTC 时区（如 `Asia/Shanghai`），调用 `RollupAndPruneTrafficHistory` 后：早于 `cutoffDate` 的小时行按 `Asia/Shanghai` 切日聚合写入 `traffic_dailies` 并从 `traffic_hourlies` 删除；`>= cutoffDate` 的小时行原封不动；早于 365 天的 `traffic_dailies` 被清除。

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/web/service -run TestTrafficLedger_`
Expected: FAIL（undefined `RecordTrafficDelta` / `model.TrafficHourly`）。

- [ ] **Step 3: Implement models, DB migration registration, and `traffic_ledger.go`**

1. 在 `internal/database/model/traffic_history.go` 定义 `TrafficHourly`（索引 `idx_traffic_hourly_key`, `idx_traffic_hourly_node`, `idx_traffic_hourly_email`）和 `TrafficDaily`（索引 `idx_traffic_daily_key`, `idx_traffic_daily_node`, `idx_traffic_daily_email`）。
2. 在 `internal/database/db.go` 的 `allModels()` 追加 `&model.TrafficHourly{}` 与 `&model.TrafficDaily{}`。
3. 在 `internal/web/service/traffic_ledger.go` 实现 `TrafficLedger`：
   - 全局互斥保护的 `pending map[TrafficLedgerKey]TrafficDelta`；忽略 `up <= 0 && down <= 0`。
   - `FlushTrafficLedger` 原子交换出当前 map，通过 `submitTrafficWriteBackground` 在单事务内按 500 条分批执行 `clause.OnConflict{Columns: ..., DoUpdates: ...}`（`up = up + excluded.up, down = down + excluded.down`）；若事务返回错误，将取出的 delta 重新加回 `pending`。
   - `RenameTrafficHistoryTx` 在调用方已有串行事务 `tx` 内先合并内存 `pending` 中 `oldEmail -> newEmail`，再用 `INSERT INTO ... SELECT ... ON CONFLICT DO UPDATE` 合并两张历史表后删除 `oldEmail` 行。
   - `TrafficHourlyCutoffDate(now, loc)`：计算 `now.In(loc)` 当天零点再减 90 天（`AddDate(0, 0, -90)`），返回 `YYYY-MM-DD` 与其零点 Unix 秒。`RollupAndPruneTrafficHistory` 在串行写事务中把 `hour_bucket < cutoffUnix` 的行按 `time.Unix(h.HourBucket, 0).In(loc).Format("2006-01-02")` 聚合 `UPSERT` 到 `traffic_dailies`，再删对应 `traffic_hourlies`，最后删除 `date < pruneDailyDate`（`AddDate(0, 0, -365)`）的 `traffic_dailies`。

- [ ] **Step 4: Run tests to verify pass**

Run: `go test -v -race ./internal/web/service -run TestTrafficLedger_`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/database/model/traffic_history.go internal/database/db.go internal/web/service/traffic_ledger.go internal/web/service/traffic_ledger_test.go
git commit -m "feat(traffic): add hourly/daily traffic ledger models and flush/rollup service"
```

---

### Task 2: 采集接入（本机 + 远端节点 + 改名钩子 + 定时刷盘与关停）

**Files:**
- Modify: `internal/web/service/inbound_traffic.go:50-165`
- Modify: `internal/web/service/inbound_node.go:420-1280`
- Modify: `internal/web/service/client_crud.go:640-700`
- Modify: `internal/web/service/client_inbound_apply.go:870-945`
- Create: `internal/web/job/traffic_ledger_job.go`
- Modify: `internal/web/web.go:340-370, 525-545`
- Test: `internal/web/service/traffic_collection_test.go`

**Interfaces:**
- Consumes: Task 1 `RecordTrafficDelta`, `FlushTrafficLedger`, `RenameTrafficHistoryTx`, `RollupAndPruneTrafficHistory`, `SnapshotPendingTraffic`
- Produces:
  - `job.NewTrafficFlushJob()` (`Run()`)
  - `job.NewTrafficRollupJob(loc *time.Location)` (`Run()`)
  - `InboundService.SetRemoteTraffic` 内部准确产出每个节点的 `(clientDeltas, inboundDeltaSum)` 记账调用（不破坏现有签名）

- [ ] **Step 1: Write failing tests in `internal/web/service/traffic_collection_test.go`**

覆盖 5 个真实路径测试：
1. `TestTrafficCollection_LocalClientAndUnattributedInbound`：调用 `InboundService.AddTraffic` 传入 `inboundTraffics`（总和 Up=500, Down=1000）和 `clientTraffics`（alice Up=300, Down=800）；断言内存账本记录 `(node=0, "alice", 300, 800)` 与 `(node=0, "", 200, 200)`，总和恰好等于入站增量，零双算。
2. `TestTrafficCollection_RemoteNodeNormalAndRenewedDeltas`：节点已有 baseline，调用 `SetRemoteTraffic` 传入增长后的用户与入站计数，验证常规增量和 `renewed` 增量正确记入 `nodeID`；再次重置用户配额（`ResetClientTraffic`）后历史账本数据保持不变。
3. `TestTrafficCollection_RemoteNodeFirstImportSkipsHistoricalTotals`：节点首次接入尚无 baseline 且带有既有大额累计流量（如 10GB），调用 `SetRemoteTraffic` 后验证历史账本增量必须为 0（Review Focus #2）；下一轮真实新增 1MB 才开始记账。
4. `TestTrafficCollection_ClientRenameMigratesHistory`：通过 `ClientService` 改名 `alice -> alice_new`（且 `alice_new` 在历史表已有旧记录），验证改名后 `traffic_hourlies` 和缓冲中流量合并到 `alice_new`，`alice` 清零。

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/web/service -run TestTrafficCollection_`
Expected: FAIL（账本尚未接入采集与改名链路）。

- [ ] **Step 3: Wire local collection, remote collection, client rename, and cron/shutdown jobs**

1. `internal/web/service/inbound_traffic.go`：在 `addTrafficLocked` 首个 durable DB 事务提交成功后，按规范化后的 `clientTraffics` 记录 `(now, 0, email, up, down)`，并对 `inboundTraffics`（仅 `IsInbound == true`）总和与用户总和求非负差额记入 `(now, 0, "", diffUp, diffDown)`。
2. `internal/web/service/inbound_node.go`：在 `SetRemoteTraffic` 事务内收集本轮真实新增的 `[]ledgerEntry`（仅当 `seen == true` 存在既有 baseline 时的正增量 `deltaUp/deltaDown`，或 `renewed == true` 时的 `canon.Up/canon.Down`；同时基于入站既有累计值与新值之差收集入站正增量之和，仅对已有入站记录非首次导入的增量计算非负差额 `email == ""`），在事务成功提交返回前统一调用 `RecordTrafficDelta`。
3. `internal/web/service/client_crud.go` 与 `internal/web/service/client_inbound_apply.go`：在改名串行事务（修改 `client_traffics` / `clients.email` 的同一事务）内，当 `oldEmail != newEmail` 时调用 `RenameTrafficHistoryTx(tx, oldEmail, newEmail)`。
4. 新增 `internal/web/job/traffic_ledger_job.go`：`TrafficFlushJob` 调用 `service.FlushTrafficLedger()`，`TrafficRollupJob` 调用 `service.RollupAndPruneTrafficHistory(time.Now(), j.loc)`。
5. `internal/web/web.go`：在 `startTask` 注册 `@every 60s` 的 `TrafficFlushJob` 和 `@daily` 的 `TrafficRollupJob(loc)`；在 `stop()` 的 `StopTrafficWriter()` 之前调用 `service.FlushTrafficLedger()`（确保停机前数据落盘）。

- [ ] **Step 4: Run tests to verify pass**

Run: `go test -v -race ./internal/web/service -run "TestTrafficLedger_|TestTrafficCollection_"`
Expected: PASS。同时跑 `go test ./internal/web/...` 确认未破坏现有节点同步与改名测试。

- [ ] **Step 5: Commit**

```bash
git add internal/web/service/inbound_traffic.go internal/web/service/inbound_node.go internal/web/service/client_crud.go internal/web/service/client_inbound_apply.go internal/web/job/traffic_ledger_job.go internal/web/web.go internal/web/service/traffic_collection_test.go
git commit -m "feat(traffic): hook local/node traffic deltas, client renames, and flush/rollup jobs"
```

---

### Task 3: 统一实时速率追踪器 `LiveSpeedTracker` 与 WebSocket 广播扩展

**Files:**
- Create: `internal/web/service/live_speed.go`
- Modify: `internal/web/job/xray_traffic_job.go:195-215`
- Modify: `internal/web/job/node_traffic_sync_job.go:35-60, 160-299`
- Test: `internal/web/service/live_speed_test.go`

**Interfaces:**
- Consumes: `xray.Traffic`, `xray.ClientTraffic`
- Produces:
  - `type LiveSpeed struct { Up int64 `json:"up" example:"131072"`; Down int64 `json:"down" example:"1048576"` }`（单位：bytes/sec）
  - `type LiveSpeedSnapshot struct { Total LiveSpeed; Users map[string]LiveSpeed; Nodes map[int]LiveSpeed }`
  - `UpdateLocalLiveSpeeds(now time.Time, windowSec float64, inboundTraffics []*xray.Traffic, clientTraffics []*xray.ClientTraffic)`
  - `UpdateNodeLiveSpeeds(now time.Time, nodeSpeeds map[int]LiveSpeed, nodeUserSpeeds map[int]map[string]LiveSpeed)`
  - `GetLiveSpeedSnapshot(now time.Time) LiveSpeedSnapshot`（15s 静默条目自动剔除/归零；多来源同用户速率按 `email` 叠加求和）
  - `ResetLiveSpeedsForTest()`
  - WebSocket `traffic` 消息新增向后兼容字段：`liveUserSpeeds`（仅速率 > 0 且遵循 `clientStatsSnapshotMaxClients` 上限）、`liveNodeSpeeds`、`liveTotalSpeed`。

- [ ] **Step 1: Write failing tests in `internal/web/service/live_speed_test.go`**

覆盖：
1. `TestLiveSpeedTracker_AggregatesLocalAndRemotePerUserAndTotal`：本机 alice 跑 100KB/s，节点 1 上 alice 同时跑 200KB/s、bob 跑 50KB/s；验证 `Users["alice"] == 300KB/s`，`Nodes[0] == 本机入站速率`，`Nodes[1] == 节点 1 入站速率`，`Total == Nodes[0] + Nodes[1]`。
2. `TestLiveSpeedTracker_ExpiresStaleEntriesAfter15s`：超过 15 秒未更新的节点或本机样本自动过期归零，活跃节点不受影响。
3. `TestLiveSpeedTracker_ZeroDeltaClearsActiveUser`：本机下一轮 5 秒轮询 alice 变成 0 流量，立即清除本机 alice 速率，不再残留。

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/web/service -run TestLiveSpeedTracker_`
Expected: FAIL（undefined `UpdateLocalLiveSpeeds`）。

- [ ] **Step 3: Implement `live_speed.go` and hook into `XrayTrafficJob` & `NodeTrafficSyncJob`**

1. 在 `internal/web/service/live_speed.go` 按 `sourceKey`（`0` 为本机，`nodeID` 为远端节点）存储最近一次 `updatedAt`、节点速率和该源下活跃用户速率；`GetLiveSpeedSnapshot(now)` 跳过 `now.Sub(updatedAt) > 15*time.Second` 的源，聚合输出 `Total`、`Users`、`Nodes`。
2. 在 `internal/web/job/xray_traffic_job.go`：用本次 poll 距上次 poll 的实际间隔（clamp 到 `[1s, 60s]`，首次默认 `5s`）调用 `service.UpdateLocalLiveSpeeds`，并在 `websocket.BroadcastTraffic` payload 中追加 `liveUserSpeeds`、`liveNodeSpeeds`、`liveTotalSpeed`。
3. 在 `internal/web/job/node_traffic_sync_job.go`：仿照现有 `prevInboundTotals` 的真实耗时归一化算法，新增 `prevNodeClientTotals` 派生各节点每用户归一化 bytes/sec，并把 `nodeInboundSpeed()` 按 `nodeID` 汇总为 `nodeSpeeds`，调用 `service.UpdateNodeLiveSpeeds`；在 `websocket.BroadcastTraffic` payload 中追加 `liveUserSpeeds`、`liveNodeSpeeds`、`liveTotalSpeed`。

- [ ] **Step 4: Run tests to verify pass**

Run: `go test -v -race ./internal/web/service -run TestLiveSpeedTracker_ && go test -race ./internal/web/job/...`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/web/service/live_speed.go internal/web/service/live_speed_test.go internal/web/job/xray_traffic_job.go internal/web/job/node_traffic_sync_job.go
git commit -m "feat(traffic): add unified local+node LiveSpeedTracker and WebSocket speed fields"
```

---

### Task 4: 流量查询聚合服务 `TrafficAnalyticsService`

**Files:**
- Create: `internal/web/service/traffic_analytics.go`
- Test: `internal/web/service/traffic_analytics_test.go`

**Interfaces:**
- Consumes: Task 1 `model.TrafficHourly`, `model.TrafficDaily`, `SnapshotPendingTraffic`, `TrafficHourlyCutoffDate`; Task 3 `GetLiveSpeedSnapshot`, `LiveSpeed`; `SettingService.GetTimeLocation`, `InboundService.GetOnlineClients`, `NodeService`
- Produces:
  - Structs (all fields carry `json:` + `example:` tags for `openapigen`):
    - `TrafficSeriesPoint` (`Timestamp int64`, `Label string`, `Up int64`, `Down int64`, `Total int64`, `Future bool`)
    - `TrafficSummaryMetrics` (`TotalUp int64`, `TotalDown int64`, `Total int64`, `PrevTotal int64`, `ChangePct *float64`)
    - `TrafficOverviewSummary` (embeds/includes `TrafficSummaryMetrics` + `ActiveClients int`, `TotalClients int`, `TopClientEmail string`, `TopClientTotal int64`, `OnlineNodes int`, `TotalNodes int`, `TopNodeId int`, `TopNodeName string`, `TopNodeTotal int64`, `LiveUpSpeed int64`, `LiveDownSpeed int64`, `HasHistory bool`, `HourlyExpired bool`)
    - `TrafficClientRankItem` (`Email string`, `Up int64`, `Down int64`, `Total int64`, `SharePct float64`, `Online bool`, `UpSpeed int64`, `DownSpeed int64`, `Deleted bool`)
    - `TrafficNodeRankItem` (`NodeId int`, `NodeName string`, `Status string`, `Up int64`, `Down int64`, `Total int64`, `SharePct float64`, `UpSpeed int64`, `DownSpeed int64`, `Deleted bool`)
    - `TrafficOverviewResponse` (`Period`, `Anchor`, `Today`, `Timezone`, `RangeStart`, `RangeEnd`, `BucketUnit`, `HourlyRetentionCutoff`, `Summary`, `Series`, `Nodes`)
    - `TrafficClientsPageResponse` (`Total`, `Page`, `PageSize`, `PeriodTotal`, `Items`)
    - `TrafficNodeBreakdownItem` (`NodeId`, `NodeName`, `Status`, `Up`, `Down`, `Total`, `SharePct`, `Deleted`)
    - `TrafficClientDetailResponse` (`Email`, `Online`, `Deleted`, `UpSpeed`, `DownSpeed`, `Summary`, `Series`, `Nodes`)
    - `TrafficClientBreakdownItem` (`Email`, `Online`, `Up`, `Down`, `Total`, `SharePct`, `Deleted`, `Unattributed`)
    - `TrafficNodeDetailResponse` (`NodeId`, `NodeName`, `Status`, `Deleted`, `UpSpeed`, `DownSpeed`, `Summary`, `Series`, `ClientsTotal`, `Clients`)
  - Methods on `TrafficAnalyticsService`:
    - `GetOverview(period, anchor string) (*TrafficOverviewResponse, error)`
    - `GetClientsPage(period, anchor, search, sortBy string, activeOnly bool, page, pageSize int) (*TrafficClientsPageResponse, error)`
    - `GetClientDetail(email, period, anchor string) (*TrafficClientDetailResponse, error)`
    - `GetNodeDetail(nodeID int, period, anchor, search string, page, pageSize int) (*TrafficNodeDetailResponse, error)`

- [ ] **Step 1: Write failing tests in `internal/web/service/traffic_analytics_test.go`**

覆盖全部关键行为与 Review Focus：
1. `TestTrafficAnalytics_DayPeriodHandlesDST23hAnd25h`：使用 `America/New_York` 分别查询春季跳时日（23 桶）和秋季回拨日（25 桶），断言桶数量、每个桶的 `Timestamp` 与聚合数值精确无误（Review Focus #1）。
2. `TestTrafficAnalytics_WeekAndMonthCross90DayCutoffNoDoubleCount`：构造跨越 `cutoffDate` 的某周/某月（前段在 `traffic_dailies`，后段在 `traffic_hourlies`，并在内存 `TrafficLedger` 放一笔当前小时未刷盘数据），断言边界日零双算、内存未刷盘实时计入、`ChangePct` 计算准确且上一周期为 0 时为 `nil`（Review Focus #4）。
3. `TestTrafficAnalytics_ClientsPageSearchSortActiveOnlyAndDeleted`：包含现存有流量用户、现存 0 流量但当前有实时速率用户、现存 0 流量离线用户、以及本周期有流量的已删用户；验证 `sort=traffic`、`sort=speed`、`activeOnly=true/false`、`search`、分页切片与 `deleted: true` 标记，同时确认未归属流量（`email == ""`）绝不出现在用户排行中。
4. `TestTrafficAnalytics_ClientAndNodeDetailsCrossBreakdown`：验证 `GetClientDetail` 返回该用户在各节点（含本机 0 与已删节点兜底名）的分摊；验证 `GetNodeDetail(0)` 和 `GetNodeDetail(nodeID)` 返回该节点下各用户排行及 `Unattributed: true` 行（当存在 `email == ""` 流量时）。
5. `TestTrafficAnalytics_DayBeyondRetentionFlagsHourlyExpired`：查询早于 `cutoffDate` 的 `period=day`，返回 `HourlyExpired: true` 而不报错。

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/web/service -run TestTrafficAnalytics_`
Expected: FAIL（undefined `TrafficAnalyticsService`）。

- [ ] **Step 3: Implement `internal/web/service/traffic_analytics.go`**

核心实现要点：
- 周期解析辅助函数 `resolveTrafficRange(period, anchor string, now time.Time, loc *time.Location)`：
  - `day`：`anchor` 当天 `[startOfDay, nextDayStart)`，按 `t = startOfDay; t < nextDayStart; t = t.Add(time.Hour)` 枚举每一个真实 UTC 整点桶（天然支持 23h/25h DST），上一周期为前一天；
  - `week`：`anchor` 所在自然周（周一 00:00 ~ 下周一 00:00，7 天），上一周期为上一自然周；
  - `month`：`anchor` 所在自然月（1 日 00:00 ~ 下月 1 日 00:00，28~31 天），上一周期为上一自然月；
  - 对每个桶判断 `Future = bucketStart.After(now)`。
- 双表+内存统一读取辅助函数 `loadTrafficBuckets(...)`：
  - `day` 模式仅查 `traffic_hourlies`（`hour_bucket >= startUnix && hour_bucket < endUnix`）并合并 `SnapshotPendingTraffic()` 中落入区间的数据；若 `anchor < cutoffDate` 标记 `HourlyExpired = true`。
  - `week`/`month` 模式将日期集合按 `< cutoffDate`（查 `traffic_dailies`）与 `>= cutoffDate`（查 `traffic_hourlies` + 合并内存缓冲，按 `time.Unix(bucket, 0).In(loc).Format("2006-01-02")` 归日）严格拆开执行，杜绝任何日期双算。
- 在线状态与现存用户/节点目录：查询 `clients`（所有现存 email）、`nodes`（所有现存节点名/状态）、`InboundService.GetOnlineClients()`、`GetLiveSpeedSnapshot(now)`；`nodeId = 0` 固定为本机（状态依据 `XrayService.IsXrayRunning()` 或 `"online"`）。
- `GetClientsPage`：先构建候选集（`activeOnly=true` 时仅保留 `periodTotal > 0` 或（当前周期含今天且 `online || speed > 0`）的用户；`activeOnly=false` 时合并所有现存用户 + 本周期有流量的已删用户），应用 `search`（不区分大小写子串匹配）、`sort`（`speed` 按 `upSpeed+downSpeed` 降序再按 `total` 降序；`traffic` 按 `total` 降序再按 `email` 升序），最后切分 `page/pageSize` 返回。

- [ ] **Step 4: Run tests to verify pass**

Run: `go test -v -race ./internal/web/service -run TestTrafficAnalytics_`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/web/service/traffic_analytics.go internal/web/service/traffic_analytics_test.go
git commit -m "feat(traffic): implement TrafficAnalyticsService for day/week/month queries and pagination"
```

---

### Task 5: HTTP 控制器、SPA 路由、OpenAPI Codegen 与文档同步

**Files:**
- Create: `internal/web/controller/traffic.go`
- Modify: `internal/web/controller/api.go:230-238`
- Modify: `internal/web/controller/spa.go:38-50`
- Modify: `tools/openapigen/main.go:100-141`
- Modify: `frontend/src/pages/api-docs/endpoints.ts`
- Test: `internal/web/controller/traffic_test.go`

**Interfaces:**
- Consumes: Task 4 `TrafficAnalyticsService`
- Produces:
  - `GET /panel/api/traffic/overview`
  - `GET /panel/api/traffic/clients`
  - `GET /panel/api/traffic/client/:email`
  - `GET /panel/api/traffic/node/:id`
  - `GET /panel/traffic` (SPA shell)
  - Regenerated `frontend/src/generated/{zod,types,schemas,examples}.ts`, `frontend/public/openapi.json`, `docs/public/openapi.json`, `docs/content/docs/en/reference/api/*`

- [ ] **Step 1: Write failing tests in `internal/web/controller/traffic_test.go`**

覆盖：
1. `TestTrafficController_EndpointsReturnExpectedPayloads`：通过 Gin `httptest` 验证 4 个接口返回 `success: true` 与完整字段结构。
2. `TestTrafficController_InvalidParamsHandledCleanly`：非法 `period`、`anchor`、非整数 `node/:id`、负数 `node/:id` 返回 `success: false`（HTTP 400 或规范错误 JSON），超界 `pageSize` 自动钳位到 100。
3. `TestTrafficController_RequiresAuth`：未登录请求被 `/panel/api` 鉴权拦截。

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/web/controller -run TestTrafficController_`
Expected: FAIL（undefined `NewTrafficController`）。

- [ ] **Step 3: Implement `traffic.go`, wire routes, register OpenAPI & regenerate**

1. 新增 `internal/web/controller/traffic.go`：实现 `NewTrafficController(g *gin.RouterGroup)` 及 4 个 handler（使用 `jsonObj` / `jsonMsg`，校验 `period`、`anchor`、`nodeID >= 0`、`page >= 1`、`1 <= pageSize <= 100`）。
2. 在 `internal/web/controller/api.go` 的 `initRouter` 注册 `NewTrafficController(api.Group("/traffic"))`；在 `internal/web/controller/spa.go` 注册 `g.GET("/traffic", a.panelSPA)`。
3. 在 `tools/openapigen/main.go` 的 `internal/web/service` `StructAllow` 中加入：`LiveSpeed`, `TrafficSeriesPoint`, `TrafficSummaryMetrics`, `TrafficOverviewSummary`, `TrafficClientRankItem`, `TrafficNodeRankItem`, `TrafficOverviewResponse`, `TrafficClientsPageResponse`, `TrafficNodeBreakdownItem`, `TrafficClientDetailResponse`, `TrafficClientBreakdownItem`, `TrafficNodeDetailResponse`。
4. 在 `frontend/src/pages/api-docs/endpoints.ts` 添加 `Traffic` 分组的 4 个端点定义。
5. 运行代码生成与文档同步：
   ```bash
   make gen && cp frontend/public/openapi.json docs/public/openapi.json && (cd docs && pnpm gen:api)
   ```

- [ ] **Step 4: Run controller & route contract tests**

Run: `go test -v ./internal/web/controller -run TestTrafficController_ && go test -v ./internal/web -run TestRouteRegistryContract`
Expected: PASS（`TestRouteRegistryContract` 证明 Gin 路由与 `endpoints.ts` 完全对齐）。

- [ ] **Step 5: Commit**

```bash
git add internal/web/controller/traffic.go internal/web/controller/api.go internal/web/controller/spa.go internal/web/controller/traffic_test.go tools/openapigen/main.go frontend/src/pages/api-docs/endpoints.ts frontend/src/generated frontend/public/openapi.json docs/public/openapi.json docs/content/docs/en/reference/api
git commit -m "feat(traffic): expose traffic analytics API endpoints and regenerate OpenAPI artifacts"
```

---

### Task 6: 前端数据层、实时速率 Hook、`uplot` 流量趋势图、路由与双语文案

**Files:**
- Create: `frontend/src/schemas/traffic.ts`
- Modify: `frontend/src/api/queryKeys.ts`
- Create: `frontend/src/api/queries/useTrafficQueries.ts`
- Create: `frontend/src/hooks/useLiveTrafficSpeeds.ts`
- Create: `frontend/src/components/viz/TrafficTrendChart.tsx`
- Create: `frontend/src/components/viz/TrafficTrendChart.css`
- Modify: `frontend/src/routes/panelRoutes.tsx`
- Modify: `internal/web/translation/en-US.json`
- Modify: `internal/web/translation/zh-CN.json`

**Interfaces:**
- Consumes: `frontend/src/generated/zod.ts` (`TrafficOverviewResponseSchema`, `TrafficClientsPageResponseSchema`, `TrafficClientDetailResponseSchema`, `TrafficNodeDetailResponseSchema`), `getSharedWebSocketClient` (`@/api/websocket`), `useTheme` (`@/hooks/useTheme`), `uplot`
- Produces:
  - Types & schemas from `@/schemas/traffic`（`TrafficPeriod = 'day' | 'week' | 'month'`, `TrafficSort = 'traffic' | 'speed'`, `z.infer` 导出类型）
  - Hooks: `useTrafficOverviewQuery`, `useTrafficClientsQuery`, `useTrafficClientDetailQuery`, `useTrafficNodeDetailQuery`, `useLiveTrafficSpeeds`
  - Component: `<TrafficTrendChart series={...} bucketUnit={...} height={...} loading={...} />`

- [ ] **Step 1: Create Zod schemas, query keys, TanStack Query hooks, and live-speed WS hook**

1. `frontend/src/schemas/traffic.ts`：直接重导出并包装生成自 `frontend/src/generated/zod.ts` 的 4 个 schema，用 `z.infer` 导出全部类型（禁止手写重复接口）。
2. `frontend/src/api/queryKeys.ts`：新增 `traffic` 键分组（`overview`, `clients`, `clientDetail`, `nodeDetail`）。
3. `frontend/src/api/queries/useTrafficQueries.ts`：封装 4 个 `useQuery`（使用 `parseMsg` 校验响应、`keepPreviousData` 避免翻页/切换周期闪烁；当查看包含今天的周期时支持温和的 30s 后台刷新）。
4. `frontend/src/hooks/useLiveTrafficSpeeds.ts`：订阅 `getSharedWebSocketClient().on('traffic', ...)` 提取 `liveUserSpeeds`、`liveNodeSpeeds`、`liveTotalSpeed` 与 WS 连接状态，并将最近更新时间暴露给页面呼吸灯；在尚未收到首帧 WS 时平滑回落至接口初始快照。

- [ ] **Step 2: Build reusable `<TrafficTrendChart />` component using `uplot`**

在 `frontend/src/components/viz/TrafficTrendChart.tsx` + `.css` 实现：
- 基于 `uplot` 绘制「下行（主色填充面积线）+ 上行（对比色折线）」双系列图表，横轴支持 `hour`（`HH:00`）与 `day`（`MM-DD`），纵轴按 `SizeFormatter.sizeFormat` 自动缩放单位（B/KB/MB/GB/TB）。
- `future: true` 的未到达桶以 `null` 断开或虚化处理，避免尾部断崖掉零。
- 自定义悬浮 Tooltip 显示精确时间桶标签、总流量、上行、下行。
- `ResizeObserver` 自适应容器宽度，跟随 `useTheme()`（亮/暗/极黑）更新网格线与文字颜色。

- [ ] **Step 3: Wire route entry and i18n keys (note: referenced in Task 7 before final commit or keep Task 6+7 cohesive)**

1. 在 `internal/web/translation/en-US.json` 和 `zh-CN.json` 添加 `menu.traffic` 与 `pages.traffic.*` 全部双语文案（将在 Task 7 全部引用，保证 `i18n-dead-keys.test.ts` 全绿）。
2. 暂不单独提交未被引用的 i18n 键（或先留到 Task 7 与页面一并提交，保证每个 commit 都通过 `i18n-dead-keys.test.ts`）。先运行 `cd frontend && npm run typecheck` 验证 Task 6 自身类型严格无报错。

Run: `cd frontend && npm run typecheck`
Expected: 0 TypeScript errors（若 `panelRoutes.tsx` 引用 `TrafficPage`，在 Task 7 创建文件后再连同 `panelRoutes.tsx` 一起通过 typecheck 与测试）。

---

### Task 7: 前端独立「流量统计」页面（方案 B）、详情抽屉、前端测试与全量验证

**Files:**
- Create: `frontend/src/pages/traffic/TrafficPage.tsx`
- Create: `frontend/src/pages/traffic/TrafficPage.css`
- Create: `frontend/src/pages/traffic/TrafficDetailDrawer.tsx`
- Modify: `frontend/src/routes/panelRoutes.tsx`
- Create: `frontend/src/test/traffic-page.test.tsx`

**Interfaces:**
- Consumes: Task 6 `useTraffic*Query`, `useLiveTrafficSpeeds`, `TrafficTrendChart`, `ClientSpeedTag`, `SizeFormatter`
- Produces: 完整可用的 `/panel/traffic` 页面与交互测试

- [ ] **Step 1: Write failing frontend tests in `frontend/src/test/traffic-page.test.tsx`**

使用 Vitest + React Testing Library + mock `HttpUtil` & `uplot`（参照 `frontend/src/test/node-history-panel.test.tsx`）覆盖：
1. 首屏渲染 4 张 KPI 卡片（实时总速率、周期总流量与环比、活跃用户与 Top 1、在线节点与 Top 1）、趋势图区域、以及左右并排的用户榜和节点榜。
2. 切换 `日 / 周 / 月` `Segmented` 与点击 `上一期 / 下一期 / 回到当前`，正确以对应 `period` 和 `anchor` 重发请求。
3. 模拟 WebSocket `traffic` 推送 `liveTotalSpeed` / `liveUserSpeeds` / `liveNodeSpeeds`，验证顶部总速率卡片、左栏用户速率与右栏节点速率即时原位刷新。
4. 左栏搜索框输入、切换 `按实时速率` 排序、勾选 `仅看活跃` 与翻页，验证携带正确查询参数调用 `/panel/api/traffic/clients`。
5. 点击左栏用户打开「用户详情抽屉」展示各节点分摊；点击右栏节点打开「节点详情抽屉」展示各用户排行及未归属流量行，再点击抽屉内某个用户直接切换为该用户的详情抽屉。
6. 冷启动无历史（`hasHistory: false`）显示友好采集提示；查询超 90 天日视图（`hourlyExpired: true`）显示超出小时保留期提示并引导切换周/月。

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/test/traffic-page.test.tsx`
Expected: FAIL（`TrafficPage` 尚未创建）。

- [ ] **Step 3: Implement `TrafficPage.tsx`, `TrafficDetailDrawer.tsx`, CSS, and register `/traffic` in `panelRoutes.tsx`**

实现要点（严格按方案 B）：
1. **顶部工具栏**：页面标题 + `Segmented`（日/周/月）+ `‹` / Ant Design `DatePicker`（按 `period` 切换 `date`/`week`/`month` picker）/ `›` + `回到当前` 按钮 + 实时呼吸灯与面板时区 Badge。
2. **4 张 KPI 卡片**（Ant Design `Row` / `Col` 响应式 4 列 → 2 列 → 1 列）：
   - 实时总速率（总速率 + `↑ / ↓`）
   - 周期总流量（总量 + `↑ / ↓` + 环比绿/红/灰徽章）
   - 活跃用户（`activeClients / totalClients` + Top 1 用户）
   - 在线节点（`onlineNodes / totalNodes` + Top 1 节点）
3. **趋势主图卡片**：嵌入 `<TrafficTrendChart />`；若 `hourlyExpired` 显示轻提示条；若 `!hasHistory` 显示首次采集友好空态。
4. **双榜并排区**（`Row gutter={[16, 16]}`，左右各 `xs={24} xl={12}`）：
   - 左栏卡片「用户流量 & 速率」：搜索 `Input.Search`（300ms 防抖）、排序 `Segmented`（周期流量 / 实时速率）、`仅看活跃` `Switch`；行内显示在线绿点、用户名（`deleted` 显示浅色 Tag）、`ClientSpeedTag`（零速显示空闲）、周期总量与上下行小字、占比进度条；底部紧凑 `Pagination`。当 `sort === 'speed'` 且收到新 WS 活跃用户变化时，节流（5s）触发 `clients` query 刷新以保持实时速率排序准确。
   - 右栏卡片「节点流量 & 速率」：排序 `Segmented`（周期流量 / 实时速率）；行内显示节点在线状态点、节点名（`nodeId === 0` 译为本机、`deleted` 标已删）、节点实时速率标签、周期总量与上下行小字、占比进度条。
5. **`TrafficDetailDrawer.tsx`**：右侧 Ant Design `Drawer`（宽度 `min(560px, 100vw)`），接收 `target: { kind: 'client'; email: string } | { kind: 'node'; nodeId: number } | null`：
   - 头部展示对象标识、实时速率、本周期总量/上下行/环比；
   - 中部复用 `<TrafficTrendChart />` 渲染专属曲线；
   - 底部展示交叉明细表（用户抽屉显示各节点分摊；节点抽屉支持搜索分页显示各用户排行，对 `unattributed: true` 显示「其他/未归属流量」不可点击，其余用户行点击调用 `onSelectClient(email)` 无缝切换抽屉目标）。
6. 在 `frontend/src/routes/panelRoutes.tsx` 的 `/clients` 或 `/nodes` 附近注册 `{ path: '/traffic', titleKey: 'menu.traffic', icon: LineChartOutlined, component: TrafficPage }`。

- [ ] **Step 4: Run frontend tests, i18n check, and full repo verification**

```bash
cd frontend && npx vitest run src/test/traffic-page.test.tsx src/test/i18n-dead-keys.test.ts
make verify && make race
```
Expected: 全部测试、`gen-check`、`lint`、`typecheck`、`build`、`build-storybook`、`docs-openapi-check` 与 `make race` 全部通过，0 报错。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/schemas/traffic.ts frontend/src/api/queryKeys.ts frontend/src/api/queries/useTrafficQueries.ts frontend/src/hooks/useLiveTrafficSpeeds.ts frontend/src/components/viz/TrafficTrendChart.tsx frontend/src/components/viz/TrafficTrendChart.css frontend/src/pages/traffic frontend/src/routes/panelRoutes.tsx frontend/src/test/traffic-page.test.tsx internal/web/translation/en-US.json internal/web/translation/zh-CN.json
git commit -m "feat(traffic): add standalone traffic analytics page with live speeds and drill-down drawer"
```

---

## Self-Review Notes
- **Spec coverage**：设计文档全部章节（导航入口、方案 B 首屏 4 KPI + uPlot 趋势 + 双榜并排 + 右侧抽屉、面板时区 `timeLocation`、两张历史表、内存缓冲 60s 刷盘+关停刷盘+实时读取合并、本机+远端采集含首次接入护栏与未归属差额、用户改名合并、90/365 天严格切分归档、统一实时速率与 WS 广播、4 个分页/详情 API、OpenAPI+docs+i18n 集成规范）均已一一映射到 Task 1–7。
- **Type & interface consistency**：`TrafficLedgerKey`/`TrafficDelta`、`LiveSpeed`/`LiveSpeedSnapshot`、4 个 API 路径与 12 个响应结构体在 Task 1–7 中名称和字段完全一致。
- **Review Focus coverage**：5 项关键风险全部绑定到了对应 Task 的明确测试用例子项。
