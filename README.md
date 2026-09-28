# supportdesk — 本地客服工单 SLA 管理工具

一个完全离线的命令行工单管理工具：逐行读取 JSON 请求、逐行返回 JSON 结果，
按北京时间工作日历累计“处理中”分钟，冻结完成时刻的耗时，并持久化工单与
完整状态变更历史。仅依赖 Go 标准库。

## 运行方法

```bash
go run ./cmd/supportdesk --state ./data
# 或编译后运行
go build -o supportdesk ./cmd/supportdesk
./supportdesk --state ./data
```

- `--state`（必填）：数据目录，状态保存在其中的 `state.json`，写入采用
  临时文件 + 原子重命名。
- 标准输入每行一个 JSON 请求；每行产出一个 JSON 响应。空行忽略；
  JSON 解析失败返回 `{"ok":false,...}` 而不退出。EOF 即正常退出，
  之后重开可继续查询与处理。

## 请求格式

所有时间字段 `at` 均为北京时间、精确到分钟：`"YYYY-MM-DDTHH:MM"`
（如 `"2026-09-28T14:30"`）。不接受秒、`Z` 或时区偏移后缀。

### 1. 初始化日历 `init`

必须在任何其他操作前成功执行一次；初始化后日历永久固定，重复 `init` 报错。

```json
{"op":"init","holidays":["2026-10-01","2026-10-02"]}
```

- `holidays`：休息日日期数组，格式 `YYYY-MM-DD`（北京日期），不可重复；
  没有休息日时传 `[]` 或省略。

### 2. 创建工单 `create`

```json
{"op":"create","id":"T-1","customer":"Acme","assignee":"alice",
 "limit_minutes":120,"at":"2026-09-28T09:00"}
```

- `id`：唯一编号，重复编号报错。
- `customer` / `assignee`：客户与负责人，均不可为空白。
- `limit_minutes`：处理时限，正整数分钟。
- 创建后初始状态为 `open`（处理中）。

### 3. 变更状态 `transition`

```json
{"op":"transition","id":"T-1","to":"waiting",
 "reason":"等客户提供日志","at":"2026-09-28T11:00"}
```

合法状态转换（其他一律拒绝，失败不修改工单与历史）：

| 当前 | 可转到 |
| --- | --- |
| `open`（处理中） | `waiting`、`done` |
| `waiting`（等待客户） | `open` |
| `done`（已完成） | 终态，不可再变 |

- `to` 为未知状态时报 `invalid status`。
- 同一工单的操作时间不得早于该工单最近一次操作时间，否则报时间倒退错误。

### 4. 查询详情 `get`

```json
{"op":"get","id":"T-1","at":"2026-09-28T17:00"}
```

查询时间不得早于该工单最近操作；查询本身不写任何记录。

### 5. 筛选列表 `list`

```json
{"op":"list","at":"2026-09-28T18:00","status":"open",
 "assignee":"alice","overtime":false}
```

- `status`、`assignee`、`overtime` 均可省略，表示不过滤该条件。
- 排序：先按创建时间，再按编号升序（稳定）。

## 响应格式

```json
{"ok":true,"op":"get","ticket":{
  "id":"T-1","customer":"Acme","assignee":"alice","status":"open",
  "limit_minutes":120,"elapsed_minutes":60,"remaining_minutes":60,
  "overtime":false,
  "created_at":"2026-09-28T09:00","updated_at":"2026-09-28T10:00",
  "history":[{"from":"","to":"open","at":"2026-09-28T09:00"}]}}
```

- 失败：`{"ok":false,"op":"...","error":"..."}`，任何失败都不会改动持久化数据。
- `list` 成功时返回 `tickets` 数组。
- 状态码语义：`open` 处理中 / `waiting` 等待客户 / `done` 已完成。

## 计时规则

- 工作时间：周一至周五（北京时间）09:00–18:00；午休不扣除。
- 休息日：周六、周日，以及 `init` 指定的日期，全天不计。
- 只累计工单处于 `open` 且落在工作时间内的整分钟：
  - `waiting` 期间不计时（等待暂停），恢复 `open` 后从恢复时刻继续；
  - 夜间、周末、休息日不计时；
  - 跨多段（open → waiting → open）分别累计后求和。
- 完成（转到 `done`）时按完成时刻**冻结**已用分钟，之后查询不再增长。
- 结果字段：
  - `elapsed_minutes`：已用分钟（非负）；
  - `remaining_minutes`：剩余分钟，不为负，耗尽后为 0；
  - `overtime`：仅当已用严格大于时限时为 `true`，恰好用尽不算超时。

## 代码结构

- `internal/calendar`：工作日历与工作分钟区间计算。
- `internal/desk`：工单模型、状态机、SLA 计算、领域服务。
- `internal/store`：`state.json` 原子持久化。
- `internal/proto`：JSON 行协议请求/响应。
- `cmd/supportdesk`：命令入口。

## 测试

```bash
go test ./...
```

覆盖：完整流转（处理/等待/恢复/完成冻结）、跨日与周末、休息日跳过、
等待暂停、恰好用尽与超时边界、非法转换、时间倒退且失败不写入、
重复编号与缺字段、组合筛选与稳定排序、持久化重开查询。
