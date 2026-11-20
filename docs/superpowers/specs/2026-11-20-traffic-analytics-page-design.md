# FluxPanel 独立「流量统计」页面设计方案

> 日期：2026-11-20
> 状态：已通过内部设计审校，待用户确认

## 1. 背景与目标

### 为什么做这个功能

FluxPanel 目前的流量信息分散在多个地方：用户实时速率和累计流量在「用户」页，单节点累计分布藏在「节点」页弹窗里，系统吞吐在「首页」。更重要的是，数据库只记录了**累计流量**（`client_traffics`、`node_client_traffics`、`inbounds`），没有按时段持久化的历史账本，一旦重置配额或想复盘某天、某周、某月的真实使用情况就无从查起。

### 核心目标

1. **第一眼看清实时态势**：打开独立页面即刻看到全站、各用户、各节点（含本机）的当前实时速率与周期消耗，无需切换多个页面。
2. **精细查询日 / 周 / 月趋势**：支持按「日（每小时）」、「周（每天）」、「月（每天）」查看与前后翻页回溯，既能看全站汇总，也能一键下钻到单个用户或单个节点。
3. **历史账本独立于配额重置**：手动或定时重置用户流量配额时，历史真实消耗记录保留不丢失。
4. **单机与多节点一致**：本地 Xray 与所有远端节点（Master/Node mTLS 架构）统一记账，实时速率与历史统计均完整覆盖本机与子节点。

### 非目标（Non-goals）

- 不做自定义多维 BI 报表编辑器、拖拽看板或 CSV/PDF 导出中心。
- 不做新的配额限速、计费账单或告警推送机制。
- 不改变现有 `client_traffics` / `inbounds` 的配额扣减、自动续期与重置语义。

### 反目标（Anti-goals）

- **绝不拖慢或锁死热路径**：每 5 秒一次的流量采集和节点同步不得因为写入历史统计而引起 SQLite 锁竞争或响应延迟。
- **绝不双算或漏算**：同一用户同时挂在多个入站或跨本机+子节点使用时，历史增量不能重复记账，也不能漏掉无用户归属的纯入站流量。
- **万级用户不退化**：大面板（10k–20k+ 用户）切换周期、搜索或接收 5 秒 WebSocket 推送时不产生 MB 级 JSON 载荷或前端卡顿。
- **冷启动不误导**：上线前不存在的历史数据不伪造、不拿累计流量倒推，页面明确展示采集起点/空态提示。

---

## 2. 界面布局与交互设计（用户已确认：方案 B）

### 导航入口

- 侧边栏新增独立菜单项「流量统计 / Traffic」，路由 `/panel/traffic`，同时注册到命令面板（Command Palette）。
- 仅使用项目现有的 Ant Design 6 + `uplot`，不引入新的 UI 框架或图表库。

### 页面结构（自上而下）

1. **顶部工具栏**
   - 粒度切换器（`Segmented`）：`日`（按小时）/ `周`（按天）/ `月`（按天）。
   - 周期导航：`上一期 ‹` ｜ 当前日期/周/月选择器（点击可直选日期，基准日期与当前「今天」统一以面板时区为准）｜ `› 下一期` + `回到当前` 快捷按钮（查看历史周期时高亮显示）。
   - 实时刷新指示灯：显示 WebSocket 实时速率连接状态与面板时区提示。

2. **第一屏核心概览（4 张 KPI 卡片）**
   - **实时总速率**：全站当前合计速率（主值显示总速率，副行拆解 `↑ 上行 / ↓ 下行`），随 5 秒推送实时刷新。
   - **本周期总流量**：当前所选日/周/月的合计流量，副行拆解 `↑ 上行 / ↓ 下行`，并显示相对上一完整周期（昨日 / 上周 / 上月）的环比变化百分比（上一周期为 0 时显示 `—`）。
   - **活跃用户**：`本周期有流量用户数 / 总用户数`，副行展示本周期 Top 1 用户名及消耗。
   - **在线节点**：`在线节点数 / 总节点数`（含本机），副行展示本周期 Top 1 节点名及消耗。

3. **全站趋势主图（uPlot）**
   - 展示当前周期内每个时间桶的流量走势（按面板配置时区 `timeLocation` 切分，完整支持 DST 23h/25h 天）：
     - `日`：00:00 ~ 23:00 的逐小时桶。
     - `周`：自然周（周一 ~ 周日）共 7 天。
     - `月`：自然月（1 日 ~ 当月最后一日）共 28~31 天。
   - 清晰区分上行与下行（Hover 浮层精确显示时间段、总流量、上行、下行），适配浅色/暗黑/极黑三套主题。
   - 当前周期尚未走完的未来时间桶以明确空值/柔和样式区分，不画误导性尖峰或断崖。

4. **双榜并排区（核心首屏视图）**
   - 桌面端左右 12/12 两栏并排，窄屏/移动端自动上下堆叠。
   - **左栏：用户流量 & 速率排行（服务端分页）**
     - 工具条：按用户名模糊搜索 + 排序切换（`按周期流量` / `按实时速率`）+ `仅看活跃` 过滤开关。
     - `仅看活跃` 明确语义：仅显示「本周期流量 > 0，或（当查看包含今天的当前周期时）当前在线 / 实时速率 > 0」的用户；关闭时包含全部现存用户（含 0 流量）及本周期有流量的已删用户，确保随时可搜到任意用户查看详情。
     - 行内容：在线状态绿点 + 用户名（已删用户标浅色标签）、**实时速率标签（↑ / ↓，无流量显示空闲）**、本周期总流量（副字显示 ↑/↓）、占全站流量比例条。
     - 服务端分页（如每页 20/50 条）保证万级用户下查询与渲染流畅；当排序选为 `按实时速率` 时，由服务端直接按 `LiveSpeedTracker` 排序分页，避免前端全量拉取。
   - **右栏：节点流量 & 速率排行**
     - 工具条：排序切换（`按周期流量` / `按实时速率`）。
     - 行内容：节点在线状态点 + 节点名称（`nodeId = 0` 显示本地化「本机 / Local」，已删节点显示历史兜底名）、**实时节点吞吐速率（↑ / ↓）**、本周期总流量（副字显示 ↑/↓）、占全站流量比例条。

5. **右侧详情抽屉（Drawer）**
   - **点击左栏任意用户** → 滑出「用户流量详情」抽屉：
     - 头部：用户名、当前实时速率、本周期总量（上行/下行）与环比。
     - 中部：当前所选日/周/月粒度的**该用户专属趋势图（uPlot）**。
     - 底部：**各节点分摊明细表**（展示该用户本周期在本机与各子节点分别消耗的上行、下行、总量及占比）。
   - **点击右栏任意节点** → 滑出「节点流量详情」抽屉：
     - 头部：节点名、在线状态、当前实时吞吐、本周期总量（上行/下行）与环比。
     - 中部：当前所选日/周/月粒度的**该节点专属趋势图（uPlot）**。
     - 底部：**该节点下用户排行明细表**（支持搜索与分页，展示本周期内各用户在该节点消耗的上行、下行、总量及占比；若该节点存在未归属到具体用户的入站流量，单独显示为「其他/未归属流量」行；点击具体用户行可直接切换至该用户的详情抽屉）。

---

## 3. 后端数据模型与采集架构

### 3.1 统一时区权威（`timeLocation`）

- 与现有 `AuditPage`、`autoRenewClients`、`PeriodicTrafficResetJob` 保持一致：**全部日/周/月边界切分、按天归档与前端基准「今天」统一以面板配置的 IANA `timeLocation` 为唯一权威时区**（未配置时回落 `time.Local`）。
- 优点：天然正确处理夏令时（DST 23h/25h 天），且 `traffic_hourlies` 与 `traffic_dailies` 在 90 天归档边界上使用完全一致的切日标准，零重叠、零漏算。

### 3.2 持久化表结构（SQLite via GORM + `internal/database/db.go` 迁移）

1. **按小时账本表 `traffic_hourlies`（`model.TrafficHourly`）**
   - `id` (uint, PK, autoIncrement)
   - `hour_bucket` (int64, UTC 整点 Unix 秒 `t.UTC().Truncate(time.Hour).Unix()`, not null)
   - `node_id` (int, not null, default 0；`0` = 本机面板，`>0` = `nodes.id`)
   - `email` (string, size 255, not null, default `""`；用户 email，空串 `""` 表示该节点上未归属到任何用户的入站差额流量)
   - `up` (int64, not null, default 0)
   - `down` (int64, not null, default 0)
   - 索引：
     - 唯一复合索引 `idx_traffic_hourly_key (hour_bucket, node_id, email)`
     - 查询索引 `idx_traffic_hourly_node (node_id, hour_bucket)`
     - 查询索引 `idx_traffic_hourly_email (email, hour_bucket)`

2. **按天归档表 `traffic_dailies`（`model.TrafficDaily`）**
   - `id` (uint, PK, autoIncrement)
   - `date` (string, size 10, `YYYY-MM-DD`, 按面板 `timeLocation` 归档, not null)
   - `node_id` (int, not null, default 0)
   - `email` (string, size 255, not null, default `""`)
   - `up` (int64, not null, default 0)
   - `down` (int64, not null, default 0)
   - 索引：
     - 唯一复合索引 `idx_traffic_daily_key (date, node_id, email)`
     - 查询索引 `idx_traffic_daily_node (node_id, date)`
     - 查询索引 `idx_traffic_daily_email (email, date)`

### 3.3 增量采集、防双算与用户改名迁移

采集点紧贴现有的两个权威事务提交路径，只在事务成功后向内存账本记账：

1. **本机流量（`node_id = 0`）**：在 `InboundService.addTrafficLocked` 提交本轮 Xray 增量事务之后：
   - 对 `clientTraffics` 中每个 `Up > 0 || Down > 0` 的规范化用户记录 `(hourBucket, 0, email, Up, Down)`，同时累计 `cliUpSum`、`cliDownSum`。
   - 对 `inboundTraffics` 中所有 `IsInbound == true` 的入站增量累计 `inUpSum`、`inDownSum`。
   - 若 `inUpSum > cliUpSum` 或 `inDownSum > cliDownSum`，将非负差额 `max(0, inUpSum-cliUpSum)`、`max(0, inDownSum-cliDownSum)` 记入 `(hourBucket, 0, "", diffUp, diffDown)`（覆盖纯入站无 email 的协议流量，同时避免与用户流量重复相加）。

2. **远端节点流量（`node_id = nodeID`）**：在 `InboundService.SetRemoteTraffic` 事务成功提交之后：
   - 记录本次事务中每个用户实际新增的正向消耗增量：普通情况取 `(deltaUp, deltaDown)`，发生远端续期/重置后重计（`renewed == true`）取 `(canon.Up, canon.Down)`，写入 `(hourBucket, nodeID, email, addUp, addDown)`。
   - 同时计算本次快照中该节点各入站的正向总增量之和 `(inUpSum, inDownSum)`，超出本轮用户总增量的非负差额记入 `(hourBucket, nodeID, "", diffUp, diffDown)`。
   - **首次接入护栏**：首次同步导入一个既有历史累计值的远端节点（baseline 尚不存在且处于首次 adopt/初始化）时，不把节点历史上已经累计的旧总数算作当前这一小时的突发增量，仅从建立 baseline 后的真实新增开始记账。

3. **用户改名迁移（与 `client_traffics` 一致）**：
   - 当管理员通过 `ClientService.Update` 或 `UpdateInboundClient` 将用户 `oldEmail` 改名为 `newEmail` 时，在同一串行写事务内同步合并历史账本：
     1. 内存 `TrafficLedger` 中将 `oldEmail` 的待刷盘增量合并到 `newEmail`；
     2. `traffic_hourlies` 与 `traffic_dailies` 中将 `oldEmail` 的行累加合并到 `newEmail`（使用 `ON CONFLICT DO UPDATE SET up = up + excluded.up, down = down + excluded.down` 后删除 `oldEmail` 行），兼容 `newEmail` 曾被已删用户占用的冲突场景。
   - 这样用户改名后，其周/月历史趋势保持完整连续，不会截断或出现多余的已删旧名。

4. **内存聚合缓冲 + 定时刷盘（`TrafficLedger`）**
   - 所有 5 秒级增量先写入进程内互斥保护的 `map[trafficLedgerKey]{up, down}`，不直接打 SQLite。
   - 后台定时任务每 **60 秒** 将当前缓冲快照取出并通过 `submitTrafficWriteBackground` 在单事务内批量 `UPSERT` 到 `traffic_hourlies`（失败则把未写入增量回滚合并回内存缓冲，下次重试）。
   - 服务关闭（shutdown hook）时强制执行一次同步 flush，避免停机丢失最后几十秒数据。
   - **实时读取一致性**：所有查询接口在汇总 `traffic_hourlies` 时，会把当前内存中尚未 flush 的增量实时合并进结果，保证前端刷新即可看到秒级最新数据。

### 3.4 历史保留与按天归档策略

- `traffic_hourlies` 保留最近 **90 个完整自然日**（按面板 `timeLocation` 的 `today - 90 days` 零点作为严格切分点 `cutoffDate`）。
- 每日定时维护任务对 `hour_bucket < cutoffStartUnix` 的 `traffic_hourlies` 按面板 `timeLocation` 归类到 `YYYY-MM-DD`，在单事务内批量 `UPSERT` 到 `traffic_dailies` 后删除这些已归档的小时行，确保任何自然日不会同时被两张表重复计入。
- `traffic_dailies` 保留 **365 天**，超期后清理。
- **查询无缝衔接**：
  - 「日」视图查询 `traffic_hourlies`（若所选日期早于 `cutoffDate`，提示超出 90 天小时明细保留期，可切换「周/月」查看按天数据）。
  - 「周 / 月」视图对 `date >= cutoffDate` 的日期从 `traffic_hourlies` 按面板时区聚合，对 `date < cutoffDate` 的日期从 `traffic_dailies` 读取，按日期严格互斥拼接，零重复零漏算。

---

## 4. 实时速率机制（单机 + 多节点统一）

1. **后端实时速率缓存（`LiveSpeedTracker`）**
   - 维护最新一轮 5 秒窗口内：
     - 每个用户的实时速率（`email -> {upPerSec, downPerSec}`），聚合自本机 Xray 5s delta 与各在线远端节点 5s 归一化用户 delta（复用 `NodeTrafficSyncJob` 真实耗时归一化算法，避免节点短暂超时后出现虚假尖峰）。
     - 每个节点的实时吞吐速率（`nodeId -> {upPerSec, downPerSec}`），其中 `nodeId = 0` 来自本机 `XrayTrafficJob` 的入站 delta，`nodeId > 0` 来自 `NodeTrafficSyncJob` 的入站归一化 delta 汇总。
     - 超过 15 秒未更新的条目自动衰减归零，避免离线节点或断开用户残留旧速率。
2. **推送与首屏加载**
   - `GET /panel/api/traffic/overview` 与 `GET /panel/api/traffic/clients` 响应直接携带当前实时速率快照，打开瞬间即可看到准确速率。
   - WebSocket `traffic` 广播消息新增向后兼容的可选字段：
     - `liveUserSpeeds`: `map[string]{up, down}`（仅包含速率 > 0 的活跃用户，且遵循现有 payload 上限护栏）
     - `liveNodeSpeeds`: `map[int]{up, down}`（包含本机 0 与各活跃节点）
     - `liveTotalSpeed`: `{up, down}`
   - 前端收到 WS 推送后原位刷新总速率卡片、节点榜与当前页可见用户的实时速率；若当前左栏按「实时速率」排序且有新活跃用户冒头，轻量节流重拉当前页分页接口即可保持排行实时准确。

---

## 5. API 契约与工程约束

新增 `TrafficController` 挂载于 `/panel/api/traffic`（受现有面板鉴权与 CSRF 中间件保护）：

1. `GET /panel/api/traffic/overview`
   - Query：`period` (`day` | `week` | `month`，默认 `day`)，`anchor` (`YYYY-MM-DD`，默认面板时区当天)。
   - Response (`TrafficOverviewResponse`)：
     - `period`, `anchor`, `today` (面板时区当天 `YYYY-MM-DD`), `timezone` (面板 IANA 时区名), `rangeStart`, `rangeEnd`, `bucketUnit` (`hour` | `day`), `hourlyRetentionCutoff` (`YYYY-MM-DD`)
     - `summary`: `totalUp`, `totalDown`, `total`, `prevTotal`, `changePct` (*float64，上一周期为 0 时为 null), `activeClients`, `totalClients`, `topClientEmail`, `topClientTotal`, `onlineNodes`, `totalNodes`, `topNodeId`, `topNodeName`, `topNodeTotal`, `liveUpSpeed`, `liveDownSpeed`, `hasHistory` (bool)
     - `series`: `[]TrafficSeriesPoint` (`{ timestamp, label, up, down, total, future }`)
     - `nodes`: `[]TrafficNodeRankItem` (`{ nodeId, nodeName, status, up, down, total, sharePct, upSpeed, downSpeed, deleted }`)

2. `GET /panel/api/traffic/clients`
   - Query：`period`, `anchor`, `search` (可选模糊搜 email), `sort` (`traffic` | `speed`，默认 `traffic`), `activeOnly` (`true` | `false`，默认 `false`), `page` (默认 1), `pageSize` (默认 25，最大 100)。
   - Response (`TrafficClientsPageResponse`)：
     - `total`, `page`, `pageSize`, `periodTotal` (本周期全站总流量，用于占比展示)
     - `items`: `[]TrafficClientRankItem` (`{ email, up, down, total, sharePct, online, upSpeed, downSpeed, deleted }`)

3. `GET /panel/api/traffic/client/:email`
   - Query：`period`, `anchor`。
   - Response (`TrafficClientDetailResponse`)：
     - `email`, `online`, `deleted`, `upSpeed`, `downSpeed`
     - `summary`: `totalUp`, `totalDown`, `total`, `prevTotal`, `changePct`
     - `series`: `[]TrafficSeriesPoint`
     - `nodes`: `[]TrafficNodeBreakdownItem` (`{ nodeId, nodeName, status, up, down, total, sharePct, deleted }`)

4. `GET /panel/api/traffic/node/:id`
   - Path：`:id` 为整数节点 ID（`0` 表示本机）。
   - Query：`period`, `anchor`, `search` (可选), `page` (默认 1), `pageSize` (默认 25，最大 100)。
   - Response (`TrafficNodeDetailResponse`)：
     - `nodeId`, `nodeName`, `status`, `deleted`, `upSpeed`, `downSpeed`
     - `summary`: `totalUp`, `totalDown`, `total`, `prevTotal`, `changePct`
     - `series`: `[]TrafficSeriesPoint`
     - `clientsTotal` (int), `clients`: `[]TrafficClientBreakdownItem` (`{ email, online, up, down, total, sharePct, deleted, unattributed }`)

### 必须遵守的仓库集成规范

- `internal/database/db.go`：注册 `TrafficHourly` 与 `TrafficDaily` 迁移。
- `tools/openapigen/main.go`：将上述响应结构体加入 `StructAllow` 并补齐 `example:` tag。
- `frontend/src/pages/api-docs/endpoints.ts`：注册 4 个新 GET 路由，满足 `TestRouteRegistryContract`。
- 运行 `make gen`，同步 `frontend/public/openapi.json` → `docs/public/openapi.json` 并运行 `cd docs && pnpm gen:api`。
- 所有前端文案同步写入 `internal/web/translation/en-US.json` 和 `zh-CN.json`，满足 `i18n-dead-keys.test.ts`。
- 遵循 TDD 与 Go/前端测试规范，覆盖：本地+远端增量入账与防双算、首次接入远端节点不把旧累计记作突发增量、重启/flush 持久化、面板时区切日/DST/自然周/自然月聚合、90 天严格按 `cutoffDate` 归档与无缝拼接、用户改名合并迁移、已删用户/节点历史保留、服务端分页/按实时速率排序，以及前端页面/抽屉交互测试。

---

## 6. 留给实现阶段决定的细节（Left to the Builder）

- `TrafficLedger` flush 周期具体配置常量（默认 60s）与 SQLite 批量 upsert 分批大小。
- uPlot 图表的具体配色 token、响应式高度、坐标轴刻度格式化细节（需适配现有亮/暗/极黑三套主题）。
- `traffic_hourlies` 归档清理任务是挂在现有 `ClearLogsJob` 还是独立 cron job 注册在 `web.go`。

## 7. 我这边先帮你定好的默认规则（Our Calls，请审阅确认）

1. **周期与时区定义**：`日` = 自然日（00:00~23:59，按小时）；`周` = 自然周（周一~周日，按天）；`月` = 自然月（1 日~月末，按天），统一采用面板配置的时区（与现有审计日志、定期重置保持一致）。
2. **历史保留时长**：按小时明细保留 90 天，超期后自动按天归档再保留至 365 天。
3. **重置、改名与删除行为**：
   - 重置用户配额、删除用户或删除节点时，历史消费记录保留不丢失（已删对象标明已删除/兜底名），确保总账准确；
   - 修改用户 `email` 改名时，历史流量同步合并迁移到新名字，周/月曲线不断裂。
4. **未归属入站流量**：纯入站协议（无具体用户 email）的流量计入全站与节点总流量，在节点详情抽屉里单列为「其他/未归属流量」，不出现在用户排行中。
5. **大面板性能优化**：左栏用户排行和节点抽屉用户明细均走服务端分页与搜索，确保用户数多时页面与 5 秒实时速率推送依然流畅。
