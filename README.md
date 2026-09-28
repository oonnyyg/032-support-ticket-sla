# Support Desk SLA（本地工单管理工具）

纯 Go 标准库实现的命令行工单管理工具，记录工单的处理与等待过程，按工作日历
计算服务时限（SLA），可相互核对当前状态、累计耗时与完整操作历史。不依赖任何
在线服务或第三方库。

## 构建与运行

```sh
go build ./cmd/supportdesk

# 每行一个 JSON 请求，每行返回一个 JSON 结果（JSON Lines）
./supportdesk --state ./data < requests.jsonl
```

- `--state`（必填）：数据目录。首次运行通过 `init` 请求初始化日历；工单与历史
  持久化保存在该目录下的 `supportdesk.json`（临时文件 + 原子重命名写入）。
- 程序持续读取标准输入直到 EOF；正常退出后重开同一数据目录可继续查询和处理。
- 时间格式统一为北京时间（UTC+8）且精确到分钟：`"YYYY-MM-DDTHH:MM"`。

## 请求格式

所有请求都是一行 JSON，公共字段为 `op` 与 `at`。

### 1. 初始化日历 `init`

必须先于其他操作执行，且只能执行一次；之后休息日不可更改。

```json
{"op":"init","rest_days":["2026-09-30","2026-10-02"]}
```

- `rest_days`：额外休息日（工作日调休等），北京时间 `YYYY-MM-DD`；
  必须是周一至周五（周末本来就休息），不可重复。

### 2. 创建工单 `create`

```json
{"op":"create","id":"T-1001","customer":"ACME公司","assignee":"alice","limit_minutes":600,"at":"2026-09-28T09:00"}
```

- `id` 唯一编号，重复编号被拒绝；`customer`、`assignee` 不能为空；
  `limit_minutes` 必须是正整数（SLA 分钟数）。
- 创建后初始状态为 `processing`（处理中）。

### 3. 变更状态 `transition`

```json
{"op":"transition","id":"T-1001","status":"waiting_customer","at":"2026-09-28T15:00","reason":"等待客户补充日志"}
```

状态取值与允许的转换：

| 当前状态 | 可转为 |
| --- | --- |
| `processing` 处理中 | `waiting_customer`、`done` |
| `waiting_customer` 等待客户 | `processing`（只能恢复处理） |
| `done` 已完成 | （不可再变更） |

- 同一工单的操作时间不得早于最近一次操作（不允许倒退）。
- 重复编号、缺少信息、非法状态转换、时间倒退一律返回错误，且不修改工单与历史。

### 4. 查询详情 `get`

```json
{"op":"get","id":"T-1001","at":"2026-09-29T12:00"}
```

### 5. 筛选列表 `list`

```json
{"op":"list","at":"2026-09-29T18:00","status":"processing","assignee":"alice","overtime":true}
```

- `status` / `assignee` 省略或为空表示不过滤；`overtime` 为布尔值，省略不过滤。
- 排序：先按创建时间升序，再按编号升序（稳定排序）。
- 查询是只读操作，不会写入任何操作记录。
- 查询时间不能早于该工单的最近操作（`list` 不能早于所有工单的最近操作）。

## 返回格式

每行一个 JSON：

```json
{"ok":true,"data":{ ... }}
{"ok":false,"error":"duplicate ticket id \"T-1001\""}
```

工单数据字段：

```json
{
  "id": "T-1001",
  "customer": "ACME公司",
  "assignee": "alice",
  "status": "processing",
  "limit_minutes": 600,
  "created_at": "2026-09-28T09:00",
  "used_minutes": 180,
  "remaining_minutes": 420,
  "overtime": false,
  "history": [
    {"at":"2026-09-28T09:00","from":"","to":"processing"},
    {"at":"2026-09-28T12:00","from":"processing","to":"waiting_customer","reason":"等待客户补充日志"}
  ]
}
```

`list` 的 `data` 为 `{"tickets":[ ... ]}`。

## 计时规则

- 工作时间：周一至周五 09:00–18:00（每天 540 分钟）；周末与 `init` 指定的
  休息日不计时，工作日 18:00 至次日 09:00 的夜间不计时。
- 只累计工单处于 `processing` 状态、且落在工作时间内的分钟；
  `waiting_customer` 等待期间完全暂停（即使跨周末、休息日、多个工作日）。
- 耗时按操作历史中的处理区间回放计算；`get/list` 以请求中的 `at` 时刻计算
  （当前处理区间从最近一次进入处理中计到 `at`）。
- 转为 `done` 时耗时冻结，此后查询永远返回该冻结值。
- `remaining_minutes = max(0, limit_minutes - used_minutes)`，始终非负。
- `overtime = used_minutes > limit_minutes`；**恰好等于时限不算超时**。

## 测试

```sh
go test ./...
```

覆盖：完整状态流转（含用尽不超时、超时、完成冻结、完成后禁止变更）、跨日/跨夜、
周末与休息日、等待暂停、非法输入与重复编号、时间倒退、失败不修改数据、
组合筛选与稳定排序，以及重开数据目录后的查询与继续处理、CLI 端到端协议。
