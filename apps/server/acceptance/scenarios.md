# hidane 验收场景

> 本文档由验收 Agent 阅读并针对真实系统执行。场景用自然语言描述意图与期望，
> 具体操作方式由验收 Agent 自行决定；判决必须附带实际观察到的证据。

## 环境速查

- 开发数据库：Postgres，`postgres://hidane:hidane@localhost:2716/hidane`（容器 `hidane-pg`，若未运行可 `docker start hidane-pg`）
- 本仓库是 pnpm monorepo：服务端命令在 `apps/server` 下执行（`pnpm dev <cmd>`）
- daemon 若设置了 `HIDANE_API_TOKEN`，API 调用需带 `authorization: Bearer <token>`
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
- 写盘版本落在 `~/.hidane/worklogs/YYYY/MM/DD/worklog.md` 且内容一致

## 场景 4A：认证边界

daemon 需以 `HIDANE_API_TOKEN=acc-test-token HIDANE_WEBHOOK_SECRET=acc-test-secret` 启动。期望：

- `/api/*` 无 token 或错 token 返回 401；正确 Bearer token 返回 200；SSE 的 `?token=` 查询参数同样有效
- `/webhook/:name` 无签名或错签名返回 401，事件**不**落日志；正确的 `x-hidane-signature`（sha256= 前缀的 HMAC-SHA256）返回 200 且事件落日志
- `/health` 始终开放

## 场景 4B：记忆蒸馏与跨日召回

用 `chat` 告诉 Primary 一条明确的、此前不存在的长期偏好（编一条具体的），然后 `pnpm dev distill --min 1`。期望：

- 偏好被提取并晋升进 `~/.hidane/memory/MEMORY.md`（带日期与 id 注释）
- `memory.candidate` 与 `memory.promoted` 事件落日志
- 之后的新 `chat` 提问相关话题时，Primary 的回答引用了该偏好（跨进程召回）

## 场景 4C：飞书连接器（本地模拟）

不需要真实飞书应用。期望：

- POST `/feishu/events` 的 `url_verification` 返回相同 challenge
- 携带正确 verification token 的 `im.message.receive_v1` 用户消息事件被接受，`connector.feishu` 事件落日志（可设 `FEISHU_VERIFICATION_TOKEN` 与假 app 凭证启动 daemon 验证；注意消息处理会尝试回调飞书 API 失败属预期，验证捕获层即可）
- 相同 event_id 的重复推送被去重（只落一条）

## 场景 4D：连接器只捕获、不判断

飞书图片下载在本地必然失败（无真实凭证），正好用来验证「读不懂的消息也不许丢」。
向 `/feishu/events` 投递一条 `message_type: image` 的用户消息事件。期望：

- `connector.feishu` 事件**照样落库**（曾经的缺陷：纯图片消息在 appendEvent 之前就被
  `if (!text) return` 丢掉，用户发的图在日志里毫无痕迹，模型只会答「没收到图片」）
- 该事件 payload 里 `imageCount` 为 0 且带 `imageFailures`，并另有一条 `agent.error`
  记录失败原因——失败必须可见，不许被裸 `catch` 吞掉
- 转给 Agent 的文本诚实说明图片无法查看，而不是谎称「请查看附带图片」
- 再投递一条 `message_type: sticker`（无 text、无图片）的消息：事件落库，但**不**触发
  任何模型调用（日志里不应出现对应的 `route.decision`）——记录归记录，唤醒是另一回事

## 场景 4E：事件流保活与异步写口

`/api/events/stream` 是前端所有实时性的唯一来源。期望：

- 订阅后立即收到 `event: hello`
- **在没有任何新事件的空闲期内，20 秒内必须收到 `event: ping`**——浏览器端的
  EventSource 在服务端进程被杀死后仍会停留在 `readyState: OPEN` 且**不触发 error**，
  客户端只能靠「静默」判断连接已死；没有 ping 就无法区分「系统很安静」和「连接已断」，
  界面会一直显示过期数据却看起来一切正常。同时也防止空闲连接被反向代理掐断。
- 有新事件时 `event: hidane` 正常推送，且 `id` 为事件 seq
- 写口是异步的：`POST /api/chat` 立刻返回 202（不等模型），随后 `user.message`
  与回复才作为事件出现——请确认返回码与事件到达确实是分离的两件事

## 场景 4F：工作项状态可改

- `PATCH /api/work-items/:id` 传 `{"status":"done"}` 返回 200，工作项状态变为 done，
  且落一条 `work_item.status_changed`（含 from/to）
- 传回 `{"status":"open"}` 可重新打开
- 传非法状态（如 `banana`）返回 400 且**不**落事件
- 未知 id 返回 404

## 场景 4G：Web 通道也能发图给多模态模型

`POST /api/chat` 接受 `{"text": "...", "images": [{"data": "<base64>", "mimeType": "image/png"}]}`。
期望（需 daemon 以 `HIDANE_PI_PROVIDER`/`HIDANE_PI_MODEL` 指向多模态模型启动）：

- 带图请求返回 202，`user.message` 事件 payload 带 `imageCount`
- 模型**真的看到了图**：自己构造一张内容明确的图（例如中间一个洋红色方块），
  问它"图里是什么颜色的形状"，回复必须与你画的内容相符，而不是"我没有收到图片"
- 只有图片、没有文字时同样接受（202），文字与图片都没有时返回 400
- 非图片 mimeType、超过 6MB、超过 4 张的部分被丢弃而不是让整条请求失败

## 场景 4H：定时连接器（调度定义与执行）

daemon 内置 15s 调度循环。通过 `/api/schedules` 定义、管理、触发。期望：

- 创建一个 `intervalSec: 15`、`action: http`、指向本机 `/health` 的调度：40 秒内
  自动 fire ≥2 次，每次落 `schedule.fired` + `connector.http`（含 status/body），
  且 triage 决策为 `scheduled-http-record-only`（wake 未设时**不**唤醒模型）
- 创建 `action: prompt` + `cron`（如 `0 17 * * *`，`timezone: Asia/Shanghai`）：
  `nextRunAt` 与时区换算一致；`POST /api/schedules/:id/run` 立即触发一次真实
  Primary 链路（`user.message` 的 source 为 `connector:schedule:<id>`），
  之后 `nextRunAt` 仍是原 cron 的下一个时刻
- 非法定义在创建时被 400 拒绝（如 `cron: "banana"`、`intervalSec: 1`、
  http 动作但 url 不是 http(s)、cron 与 intervalSec 同时给或都不给）
- PATCH `enabled: false` 后 `nextRunAt` 变 null，调度循环不再触发它
- DELETE 落 `schedule.deleted` 事件
- 结束后删除你创建的调度，不要留下每 15s 打点的常驻任务

## 场景 4I：长回复不截断、执行可中止、记忆可手写

- **长回复**：`chunkText` 把超长文本按段落切块而非截断。构造一段 >8000 字的文本，
  确认切块后**每块不超上限、拼回来内容不丢**（真实事故：8000 字回复在飞书被 `slice(0,4000)`
  砍掉一半，用户读到半截以为系统卡死，在执行早已成功 80 分钟后问「你是不是卡住了？」）
- **中止执行**：让一个工作项跑起来（要求它做几十次工具调用），在执行中
  `POST /api/work-items/:id/cancel`。期望：先落 `execution.cancelled`（意图在前），
  执行**数秒内**结束并落 `execution.finished`，且 `cancelled: true` / `ok: false`
  ——注意 `abort()` 会让 agent 自然 idle，若按「谁先完成」判定会把被中止的执行
  错记为成功，结果标签必须跟随用户意图。没有执行在跑时 cancel 返回 409 且不落事件。
- **手写记忆**：`POST /api/memories` 写入一条，出现在 `/api/memories` 列表中，
  并落 `memory.promoted` 且带 `manual: true`（与蒸馏产出可区分）；空内容返回 400。

## 场景 4：事件不灭与重放

期望：

- 事件被消费后依然留在日志里（append-only）
- 把某个消费者的游标重置后，能重新读到历史事件（重放语义）。
  可直接操作 cursors 表验证，注意别破坏 triage 消费者的现网状态（可用一个临时消费者名验证）。
