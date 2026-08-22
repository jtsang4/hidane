# hidane 验收场景

> 本文档由验收 Agent 阅读并针对真实系统执行。场景用自然语言描述意图与期望，
> 具体操作方式由验收 Agent 自行决定；判决必须附带实际观察到的证据。

## 环境速查

- 开发数据库：Postgres，`postgres://hidane:hidane@localhost:2716/hidane`（容器 `hidane-pg`，若未运行可 `docker start hidane-pg`）
- CLI：在仓库根目录用 `pnpm dev <command>`（`chat` / `items` / `events` / `log` / `daemon` / `init`）
- daemon HTTP 端口：2718（`/health`、`POST /webhook/:name`）
- 查询数据库可用：`docker exec hidane-pg psql -U hidane -d hidane -c "..."`
- 工作区目录：`~/.hidane/workspaces/<work_item_id>/`；日志投影：`~/.hidane/worklogs/`
- 注意：`chat` 会触发真实 LLM 调用（primary 路由 → manager 规划 → worker 执行），单次可能需要 1-3 分钟

## 场景 1：快车道完整闭环

用 `chat` 提出一个需要实际动手的小任务（例如：创建一个输出当前日期的 shell 脚本并运行验证）。
期望：

- primary 将其路由为新工作项（而不是直接回复敷衍）
- 工作项拥有自己的线程和工作区目录
- worker 真的在工作区产出了文件，且文件内容与任务相符
- 事件日志里有完整链条：`user.message` → `route.decision` → `execution.started` → `execution.finished`（ok=true）→ `agent.reply`
- 最终回复内容与实际产物一致（不是编造的）

## 场景 2：后台车道与分诊

启动 daemon，向 webhook 端点投递一条事件。期望：

- webhook 立即被接受并落日志（`connector.webhook`），此时不阻塞、不判断
- 分诊循环在几秒内产出 `triage.decision`，webhook 规则为唤醒 primary
- primary 对该外部事件产出了合理的回应（`agent.reply`，内容与事件相关，非乱答）
- 心跳事件（`connector.heartbeat`）只被记录，分诊决定为 record，不唤醒任何模型
- `/health` 返回数据库正常
- 结束后清理你启动的后台进程

## 场景 3：日志投影可重建

期望：

- `pnpm dev log` 渲染出的当日工作日志包含主线程和场景 1 的工作项分区，内容能对应上真实发生的事
- 写盘版本落在 `~/.hidane/worklogs/YYYY/MM/` 下且内容一致

## 场景 4：事件不灭与重放

期望：

- 事件被消费后依然留在日志里（append-only）
- 把某个消费者的游标重置后，能重新读到历史事件（重放语义）。
  可直接操作 cursors 表验证，注意别破坏 triage 消费者的现网状态（可用一个临时消费者名验证）。
