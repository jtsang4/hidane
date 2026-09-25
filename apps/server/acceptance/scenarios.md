# hidane 验收场景

> 本文档由验收 Agent 阅读并针对真实系统执行。场景用自然语言描述意图与期望，
> 具体操作方式由验收 Agent 自行决定；判决必须附带实际观察到的证据。

## 环境速查

- 开发数据库：Postgres，`postgres://hidane:hidane@localhost:2716/hidane`（容器 `hidane-pg`，若未运行可 `docker start hidane-pg`）
- 本仓库是 pnpm monorepo：服务端命令在 `apps/server` 下执行（`pnpm dev <cmd>`）
- daemon 若设置了 `HIDANE_API_TOKEN`，API 调用需带 `authorization: Bearer <token>`
- CLI：在仓库根目录用 `pnpm dev <command>`（`chat` / `items` / `events` / `log` / `daemon` / `init`）
- 运行模型：每个 agent（`primary`、`manager:<wi>`）有一个收件箱，它是事件日志的派生视图
  （`events.mailbox` + `cursors` 里的 `mailbox:<地址>` 游标）。一个 turn 取走积压的全部消息，
  只做决策、不等待；worker 结果以 `execution.finished` 投递回 Manager 的收件箱。
  `chat` 在没有 daemon 时自己跑循环，有 daemon 时只投递并跟随回复
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
- 事件日志里有完整链条：`user.message`（主线程，mailbox=primary）→ `route.decision` →
  `message.attributed`（created=true）→ 转发到 Manager 收件箱的 `user.message` → `manager.decision` →
  `execution.started` → `execution.finished`（ok=true，mailbox=`manager:<wi>`）→ 第二个 `manager.decision` → `agent.reply`
- 所有回复的 `payload.root` 都指向最初那条 `user.message` 的 id（界面靠它把回复放在问题下面）
- 最终回复内容与实际产物一致（不是编造的）

## 场景 2：后台车道与分诊

启动 daemon，向 webhook 端点投递一条事件。期望：

- webhook 立即被接受并落日志（`connector.webhook`），此时不阻塞、不判断
- 分诊循环在几秒内产出 `triage.decision`，webhook 规则为唤醒 primary；这条决策本身就是投给
  primary 收件箱的消息（`mailbox = 'primary'`），分诊循环**不等** primary 处理完
- primary 对该外部事件产出了合理的回应（`agent.reply`，`payload.rootKind = external`，内容与事件相关，非乱答）
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
- 写口是异步的：`POST /api/chat` 立刻返回 202 与 `messageId`（不等模型），`user.message`
  已落库，回复稍后作为 `payload.root = messageId` 的事件出现——请确认返回码与回复到达确实是分离的两件事

## 场景 4F：工作项状态可改

- `PATCH /api/work-items/:id` 传 `{"status":"done"}` 返回 200，工作项状态变为 done，
  且落一条 `work_item.status_changed`（含 from/to）
- 传回 `{"status":"open"}` 可重新打开
- 传非法状态（如 `banana`）返回 400 且**不**落事件
- 未知 id 返回 404
- 通过主会话 `chat` 请求“把当前所有打开的工作项关停/关闭”时，Primary 应直接执行
  批量状态变更，不再回复“没有相应能力”；当时所有 open 工作项均变为 closed，
  每个变更都有 `work_item.status_changed`（source 为 `agent:primary`），主线程收到
  一条包含变更数量的确认回复。该操作不是删除，也不应启动 Manager/Worker。
- 通过主会话请求将一个已完成或已归档的工作项重新打开时，Primary 应使用其真实 ID
  将状态改回 open；请求“所有已有工作项”时可用批量状态操作，不能凭空编造 ID。

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
  `nextRunAt` 与时区换算一致；`POST /api/schedules/:id/run` 立即返回（状态 `posted ev_…`），
  向 primary 收件箱投递一条 `schedule.prompt`（source 为 `connector:schedule:<id>`），随后
  Primary 的回复以它为 root 出现；之后 `nextRunAt` 仍是原 cron 的下一个时刻
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
  Manager 收到被取消的结果时按规则直接回复「执行已取消。」，不调用模型。
- 也应能在主会话中说“停止这个正在运行的工作项”；Primary 直接发出同样的取消意图，
  不应把停止请求转成新的 Manager/Worker 执行。
- **手写记忆**：`POST /api/memories` 写入一条，出现在 `/api/memories` 列表中，
  并落 `memory.promoted` 且带 `manual: true`（与蒸馏产出可区分）；空内容返回 400。

## 场景 4J：工作区产物可读可下载（含路径穿越防护）

让一个工作项产出几个文件（含子目录、文本与非文本各一）。期望：

- `GET /api/work-items/:id/files` 列出产物（含子目录路径），**不含** `node_modules` / `.git`
- `GET /api/work-items/:id/file?path=notes/detail.md` 返回文本内容
- **路径穿越必须全部被拒（403）**：`../../../etc/passwd`、URL 编码变体、绝对路径
  `/etc/passwd`、`notes/../../../../etc/passwd`。这是本功能的安全边界，
  路径直接来自 URL；判定必须基于「解析后的绝对路径是否仍在工作区内」，
  而不是检查输入里有没有 `..`
- 非文本文件返回 `reason: "binary"`、超大文本返回 `reason: "too-large"`（不内联）
- `?download` 返回附件流；无 token 一律 401

> **清理约定**：任何写入 MEMORY.md 的场景（4B、4I）在结束前必须把自己写的记忆
> `forget` 掉。留下的记忆会注入后续每一次路由——曾有一条「回复开场白永远用『好的』」
> 的验收残留，让之后所有回复都以「好的」开头。

## 场景 4K：工作项可直接创建、可归档

不是每个任务都从对话开始。`POST /api/work-items` 直接开一个工作项。期望：

- 只给 `title` → 201，工作项 status 为 `open`，**不**触发 Manager（没有 brief 就没有派活）
- 给 `title` + `brief` → 201，brief 作为「对这个工作项说的话」落在主线程（带 `target`），
  并记一条 `message.attributed`（by=explicit），再投递到 Manager 收件箱；Manager 真被调起
  （可观察到 `manager.decision` 或后续执行事件）
- 通过主会话说“先记录这个工作项，暂时不要开始”时，Primary 应创建 open 工作项并记录
  deferred 消息，但不启动 Manager/Worker；后续明确要求开始时仍可正常路由。
- `title` 为空或纯空白 → 400
- `PATCH {status:"closed"}` 归档后：`GET /api/work-items` 默认列表**不含**它，
  `GET /api/work-items?all` **含**它，且事件与工作区产物均保留（归档不是删除）
- `closed` 与 `done` 是两种状态，不要混用

## 场景 4L：定时任务的运行历史可读回

`GET /api/schedules/:id/runs`。期望：

- 建两个调度 A、B，分别触发（A 两次、B 一次）→ A 的 runs **只**含 A 的事件，
  绝不混入 B 的；含 `schedule.fired` 与对应结果事件
- 顺序为**新的在前**（历史从下往上读是错的）
- 未知 id → 404
- 结束后删除你创建的调度

## 场景 4M：飞书回复以卡片 2.0 下发

飞书 msg_type=text 会把 Markdown 当字面文本显示（一堆 `#` 和 `**`）。
本地模拟一条入站消息，检查出站投递形态。期望：

- 出站 `msg_type` 为 `interactive`（不是 `text`）
- 卡片 JSON 的 `schema` 为 `"2.0"`（**1.0 只支持 Markdown 子集**，列表与表格会退化成
  字面文本——这是本场景要守住的回归）
- 卡片正文元素 `tag` 为 `markdown`
- 代码围栏（```）在发送前被压平为缩进块（飞书两个卡片版本都不渲染 CodeBlock）
- 工作项线程根消息「📋 wi_x — title」仍是 `text`（它是回复话题挂载点，不是正文）

## 场景 4N：并发事件流不会返回空 body

SSE 曾在并发下出现「200 头 + 空 body」——首次写入前先查库，查询失败时响应已提交。
期望：

- 同时开 8 条 `/api/events/stream` 连接，**每条**都能读到首个 `hello` 事件（无空 body）
- 期间正常发一条 chat，各连接都能收到新事件
- 全部断开后 daemon 仍健康（`/health` ok）

## 场景 4O：对话历史分页加载

首页曾一次性拉 200 条事件，并用平滑滚动跳到底部——观感是「从第一条一路加载滚到最后一条」。
现在首屏只取最新一页，更早的历史按需往上翻。期望：

- `/api/events?page=1&thread=main&kind=user.message,agent.reply&limit=5` 只返回这两种 kind，
  且 `hasMore` 正确。**kind 过滤必须发生在 SQL 里**：若放到取回之后再筛，一页 N 行能渲染出
  几条气泡就不可知，甚至出现一页全是 `route.decision`、翻页看起来失效
- 单个 kind（`kind=execution.finished`）语义不变，事件页的筛选照旧可用
- `/api/events?page=1&conversation=1` 返回对话视图：主线程上说的话、归属记录，加上任何线程上的回复
- 工作项详情 `/api/work-items/:id` 默认不再返回整条线程：带 `limit` 时只回该工作项的最新若干条事件
  （按 work_item_id，不分线程），并给出 `hasMore`；更早的部分用 `/api/events?item=<id>&before=` 往回翻
- 详情返回的 `running` 来自 `executions` 表（持久化），不是从事件窗口推断的。
  **这是本场景要守住的回归**：长执行会把自己的 `execution.started` 挤出窗口，
  一旦改回从事件推断，运行中的工作项会显示成空闲、「中止执行」按钮随之消失
- 往回翻页时，最新一页会随实时事件前移；两者以 id 取并集而不是直接拼接，
  否则夹在中间的事件不属于任何一页而被静默丢掉（seq 是全局的，看相邻值发现不了这个洞）

## 场景 4P：历史可按地址访问（搜索、窗口、日期）

主会话只追加、会无限变长；历史不能只靠「往上翻」和「只搜已加载的部分」。期望：

- `GET /api/conversation/search?q=<词>` 搜的是**全部**历史而不是已加载的页：多个词按空格取交集，
  新的在前，`before=<seq>` 翻页且 `hasMore` 正确；第一页同时返回标题匹配的工作项（含已归档的）。
  `%`、`_` 按字面匹配；子任务给父任务的汇报（`child: true`）与旧运行时的线程副本不出现在结果里
- `/api/events?page=1&conversation=1&around=<事件 id>` 返回以该事件为中心的一段，带 `hasMore`（更早）
  与 `hasNewer`（更新）；`after=<seq>` 往新的方向翻，直到 `hasNewer=false`。未知 id → 404
- `GET /api/conversation/days?tz=<IANA 时区>` 按读者时区列出有对话的每一天（新的在前），带条数与当天第一条的 id；
  非法时区按 UTC 处理，不报错、不进 SQL
- 对话分页响应带 `titles`：页中提到的每个工作项都有标题，**包括早已归档、看板上已经没有的**
- `origin=person` 只保留人说的话和对它的回答，去掉对 webhook / 定时任务的回复

## 场景 4Q：隐藏说错的话

`POST /api/messages/:id/redact` 隐藏人在主线程说过的一句话（如贴错的密钥）。期望：

- 落一条 `message.redacted`（`of` 指向原消息）；原消息那一行**不被改写**（数据库里原文仍在——日志只追加），
  但之后所有读取方都看不到原文：`/api/events`（含 SSE）、搜索、工作日志投影、Primary 的上下文、记忆蒸馏。
  转给工作项 Manager 的那份副本同样被遮住
- 被隐藏的消息在读取结果里 `payload.redacted = true`、`text` 为空，`root`/`target` 等结构字段保留，对话仍能正确分组
- 重复隐藏不再落事件；非人说的话（如 `agent.reply`）或不存在的 id → 404

## 场景 4R：Primary 的上下文有界，更早的内容靠检索

Primary 不再依赖一个无限增长的模型会话：每个 turn 新开会话，从日志重建「最近的对话」。期望：

- `GET /api/conversation/context` 给出 Primary 当前视野的起点 `fromId`（最近若干轮，有条数与字数上限，
  不含被隐藏的消息）；界面在这条消息上方标出「助手现在只参考从这里往下的对话」
- 每个 turn 在会话目录留一个独立的 trace 文件；重启前后 Primary 的行为一致（上下文来自日志，而非进程内存）
- 问一件远在视野之外的旧事（例如很早之前某个工作项里的具体字符串）：Primary 先产出 `recall`
  效果并**立即结束这个 turn**（不等待），随后一条 `conversation.recalled`（投给 `primary` 收件箱，
  带 `query` 与检索结果）触发下一个 turn，回答挂在最初那条消息下（`payload.root` 为原消息 id），内容与历史相符。
  对 recall 结果不会再次 recall

## 场景 5A：任务运行时对话不被阻塞

让一个需要几分钟的任务跑起来（例如「写个脚本每秒打印一次，运行 2 分钟」），在它执行期间再问一个
无关的小问题。期望：

- 小问题在数秒内得到回答（`agent.reply` 的 root 是这条小问题），不必等任务结束
- 任务结果稍后到达，其 `agent.reply` 的 root 仍是最初那条任务消息，而不是最新的消息
- `/api/status` 的 `runtime` 显示有 worker 在跑，同时 primary 收件箱没有积压

## 场景 5B：分钟级补充被合并，而不是各自返工

对同一个工作项在它执行期间连续补充两句（直接对工作项说：`POST /api/chat` 带 `target`）。期望：

- worker 运行中收到的话被**直接转给正在运行的执行**（`execution.steered`，不产生新的 `manager.decision`）
- 最终产物体现了补充的要求（例如补充「支持 --dry-run」，脚本里就有这个参数）
- 在 Manager 规划期间（尚未派出 worker）到达的几句话，下一个 turn 一次取走：
  只有一个 `manager.decision` 的 `of` 同时包含这几条消息

## 场景 5C：归属有歧义时不瞎猜，可改派

先建两个相似的工作项（如「做 login.html」「做 register.html」），再说一句含糊的话（「按钮颜色再深一点」）。期望：

- Primary 产出 `attribution.ambiguous`（带候选项），**不**把消息投给任何一个 Manager
- 通过 `POST /api/messages/:id/route {workItemId}` 选定后，落 `message.attributed`（by=user）并投递给对应 Manager
- 再改派到另一个工作项：新的 `message.attributed` 带 `previous`，原事件保留不改（只追加）
- 改派到 `new` 会新建工作项并投递
- 界面上：用户消息下方显示归属标签，歧义时显示候选按钮，不出现重复的问题气泡

## 场景 5D：问题逐层上报到人

提一个缺信息就做不了的任务（如「把 register.html 部署到我的服务器」，不给地址）。期望：

- Manager 不会静默停住：要么派 worker，要么回复，要么上报；只写「当前理解」的 turn 会被自动追问一次
- 上报路径：`escalation.raised`（投给父级收件箱，顶层即 primary）→ primary 按规则（不调模型）
  在主线程落 `escalation`，带 `question`、`path`（每层写明已尝试过什么）与 `workItemId`
- 看板 `/api/board` 中该卡片 `state = waiting`，`escalation.question` 即该问题
- 用 `POST /api/chat {replyTo: <escalation id>}` 回答后，消息投给该工作项的 Manager（payload 带 `answers`），
  卡片不再是 waiting，任务继续推进

## 场景 5E：捕获阶段的规则可以拦下动作

`POST /api/policies {"pattern":"\\brm\\b","reason":"不允许删除文件"}` 加一条全局规则，然后让某个工作项
「用 rm 删掉工作区里的某个文件」。期望：

- 文件仍在；落 `policy.blocked`（payload 含 `rule` 与规则原文 `reason`，不含守卫的英文前缀）
- 该执行的 `execution.finished.payload.policyBlocks` 非空，Manager 据此回复或上报
- 规则只做匹配，不经过模型；删除规则后同样的操作可以执行
- 结束后删除你加的规则

## 场景 5F：扇出成子任务，取消沿树向下

让一个任务拆成几个互相独立的部分（「分别调研 A、B、C，最后给对比表」）。期望：

- Manager 用子工作项扇出（`work_items.parent_id` 指向父项），每个子项有自己的工作区与 Manager
- 子项完成后 `done`；全部结束时父项收到一条 `children.settled`（含每个子项的结果），随后给出汇总
- 子项的回复带 `payload.child = true`，不在主对话里出现；父项的汇总以最初的消息为 root
- 看板上父项在子项运行时为 `delegated`
- 另起一次扇出，在子项运行时 `POST /api/work-items/<父项>/cancel`：每个仍在执行的子项都落
  `execution.cancelled`（payload.via 为父项 id），随后各自的 `execution.finished` 带 `cancelled: true`

## 场景 5G：重启不丢事

让一个长执行跑起来，然后 `kill -9` daemon（连同 worker 子进程）再启动。期望：

- 启动日志写明「N execution(s) lost to the last restart」
- 该执行在 `executions` 表里为 `lost`，并有一条 `execution.finished`（`lost: true`）投递给其 Manager
- Manager 据此决定重试或说明情况——不会有一个永远「运行中」、无人跟进的执行
- 重启前积压在收件箱里的消息（游标之后的事件）在重启后被处理

## 场景 5H：因果链有上限

把 `HIDANE_MAX_HOPS` 调小（如 3）启动 daemon，让一个会多轮执行的任务跑起来。期望：

- 超过上限时消息不再投递，改为主线程上的一条 `escalation`（`reason: budget`）
- 同理 `HIDANE_MAX_EXECUTIONS_PER_ITEM` 用尽后不再派 worker，而是上报
- 人回复该工作项后可以继续（新的因果链从 0 开始）

## 场景 5I：界面（需真实浏览器）

打开 Web 界面，验证：

- 对话流里每条消息下方有归属标签（归入「X」· 自动判断 / 你指定的 / 按当前聚焦 · 改）
- 新建的任务以卡片出现在创建它的那条消息下方，状态原位更新（规划中 → 运行中 → 空闲 / 等你回答）
- 迟到的回复出现在它所回答的消息下面；若该位置不在视野内，底部出现一行通知，点「查看」跳过去
  （只滚动对话区，外层页面不跟着动），超过 3 条时合并为「还有 N 条更新」
- 顶部「进行中」栏列出运行中、等回答和有新进展的任务；点击打开右侧聚焦面板（移动端全屏），
  输入框自动变为「发给「X」」，面板内有对话、产物、执行时间线
- 「回答」按钮把输入框切到「回答「X」的问题」，发送后问题关闭
- 规则页可以增删规则；中英文切换后界面文案全部翻译（任务标题等内容保持原文）
- 顶部搜索框搜的是全部历史：结果列出匹配的工作项与对话（高亮命中词、带时间），点一条对话结果会打开
  以它为中心的一段历史（地址带 `?at=`，可复制分享），该消息在视野内并短暂高亮；顶部提示「正在查看较早的对话」，
  往下滚会继续加载更新的内容，接上最新时自动回到实时模式；「返回搜索」能回到刚才的结果
- 只要不在最新位置（往上翻了，或正在看较早的一段历史），输入框正上方就出现「回到最新」按钮；
  在底部时不出现。点击后回到最新消息并贴底：往上翻的情况下已加载的历史保留，看历史窗口的情况下地址里的 `?at=` 被清掉；
  它与底部的「有新回复」通知上下叠放，互不遮挡
- 「按日期」列出有对话的日子（按月分组、带条数），选一天跳到那天的第一条；对话流里每天之间有日期分隔线
- 用户消息下有「复制链接」「隐藏」两个小按钮；隐藏前有确认说明（原始记录仍在日志中），隐藏后显示「这条消息已隐藏」
- 工作项面板里「在对话中查看来源」跳到创建它的那条消息，即使那条消息很久以前、当前没有加载
- 很久以前、没有被关联上回答的旧消息不显示「正在判断归属…」
- 宽屏下对话内容居中限宽；宽表格可横向滚动且有可见滚动条

## 场景 4：事件不灭与重放

期望：

- 事件被消费后依然留在日志里（append-only）
- 把某个消费者的游标重置后，能重新读到历史事件（重放语义）。
  可直接操作 cursors 表验证，注意别破坏 triage 消费者的现网状态（可用一个临时消费者名验证）。
