# hidane 验收场景

> 本文档由验收 Agent 阅读并针对真实系统执行（`make acceptance` 用本机 `claude` CLI 充当验收 Agent）。
> 场景用自然语言描述意图与期望，具体操作方式由验收 Agent 自行决定；判决必须附带实际观察到的证据。
> 只有违反写明的期望才判 FAIL；期望之外的观察（措辞、体验、风险、想法）写进 notes，不判 FAIL。
> hidane 之外的故障（模型网关 5xx、限流、CLI 未登录）不算 FAIL：重试，仍不行就判 BLOCKED 并写明原因。
> 按改动选择范围：`make acceptance ARGS="--changed origin/main"` 或 `ARGS="--only 4F,6E"`；完整跑一遍留给发版前。

## 环境速查

- hidane 是一个 Go 二进制：无参数启动桌面应用（Wails v3）；子命令即 CLI。验收用无 GUI 的
  `bin/hidane-nogui`（`make build-nogui` 产出，内嵌前端），命令：`serve` / `chat` / `items` / `events` /
  `log` / `archive` / `distill` / `model [--ping] [--role R]` / `agents` / `memories` / `forget`
- 数据全在 `HIDANE_HOME`（默认 `~/.hidane`）：SQLite 数据库 `hidane.db`（事件日志 + 少量状态表）、
  `settings.json`（provider 与角色配置，权限 0600）、`POLICY.json`、`memory/MEMORY.md`、
  `workspaces/<wi>/`、`worklogs/YYYY/MM/DD/`。**每个场景用 /tmp 下的新 HIDANE_HOME**，不要碰用户自己的 `~/.hidane`
- 查日志用 `hidane events --tail N [--item ID]`、`GET /api/events`；需要直接查库可用
  `sqlite3 $HIDANE_HOME/hidane.db`（只读查询；事件表有触发器，UPDATE/DELETE 会被拒绝）
- `hidane serve --addr 127.0.0.1:<端口>`：同一个 http.Handler 提供 Web 界面、`/api/*`、`/health`、
  `/webhook/:name`。`/api/*` 始终需要 Bearer token：设了 `HIDANE_API_TOKEN` 就用它，否则启动时随机生成并打印
  `?token=` 链接；SSE 也接受 `?token=`。同一 HIDANE_HOME 只允许一个 runtime（文件锁）
- 运行模型：每个 agent（`primary`、`manager:<wi>`）有一个收件箱，它是事件日志的派生视图
  （`events.mailbox` + `cursors` 里的 `mailbox:<地址>` 游标）。一个 turn 取走积压的全部消息，
  只做决策、不等待；worker 结果以 `execution.finished` 投递回 Manager 的收件箱。
  `chat` 在没有 runtime 时自己跑循环，有 runtime（桌面应用或 serve）时只投递并跟随回复
- 每个角色（primary / manager / worker / distiller）跑在一个本机 agent CLI 上：`claude`、`codex` 或 `pi`，
  可选 provider / model / effort，见 `settings.json` 或界面「设置」页。`bin/fake/{claude,codex,pi}` 是
  按真实协议实现的假 CLI（`make fakeagent`），写入 `settings.json` 的 `binaries` 即可零成本跑通全链路；
  用真实 CLI 时会产生真实模型调用，单次链路可能需要 1–3 分钟
## 场景 1：快车道完整闭环

用 `hidane chat` 让它开一个任务做件事（例如：「开个任务：创建一个输出当前日期的 shell 脚本并运行验证」）。
期望：

- primary 将其路由为新工作项（而不是直接回复敷衍）
- 工作项拥有自己的线程和工作区目录
- worker 真的在工作区产出了文件，且文件内容与任务相符
- 事件日志里有完整链条：`user.message`（主线程，mailbox=primary）→ `route.decision` →
  `message.attributed`（created=true）→ 转发到 Manager 收件箱的 `user.message` → `manager.decision` →
  `execution.started` → `execution.finished`（ok=true，mailbox=`manager:<wi>`）→ 第二个 `manager.decision` → `agent.reply`
- 所有回复的 `payload.root` 都指向最初那条 `user.message` 的 id（界面靠它把回复放在问题下面）
- 最终回复内容与实际产物一致（不是编造的）
- `hidane chat` 等到 Manager 的最终回复（「已完成…」一类）打印出来才退出，不会停在「已安排执行」

## 场景 1B：小事 Primary 自己动手

Primary 和 Manager 都带工具（各 CLI 的 bypass 模式，只受闸门的高危命令与规则约束）。期望：

- 问一个看一眼就能答的问题（例如「/tmp 下某个你刚建的文件里写了什么」）：Primary 自己用工具查完直接回答，
  不开工作项；它的工具调用记为 `agent:primary` 的 `side_effect.intent` / `side_effect.result`；回答与文件内容一致
- 明显要多步完成、值得跟进的活，仍然开工作项交给 worker

## 场景 2：后台车道与分诊

用 `hidane serve`（设置 `HIDANE_WEBHOOK_SECRET`）启动，向 webhook 端点投递一条带正确签名的事件；未设置 secret 时 webhook 一律 403。期望：

- webhook 立即被接受并落日志（`connector.webhook`），此时不阻塞、不判断
- 分诊循环在几秒内产出 `triage.decision`，webhook 规则为唤醒 primary；这条决策本身就是投给
  primary 收件箱的消息（`mailbox = 'primary'`），分诊循环**不等** primary 处理完
- primary 对该外部事件产出了合理的回应（`agent.reply`，`payload.rootKind = external`，内容与事件相关，非乱答）
- 心跳事件（`connector.heartbeat`）只被记录，分诊决定为 record，不唤醒任何模型
- `/health` 返回数据库正常
- 结束后清理你启动的后台进程

## 场景 3：日志投影可重建

期望：

- `hidane log` 渲染出的当日工作日志包含主线程和场景 1 的工作项分区，内容能对应上真实发生的事
- 写盘版本落在 `~/.hidane/worklogs/YYYY/MM/DD/worklog.md` 且内容一致

## 场景 4B：记忆蒸馏与跨日召回

用 `chat` 告诉 Primary 一条明确的、此前不存在的长期偏好（编一条具体的），然后 `hidane distill --min 1`。期望：

- 偏好被提取并晋升进 `~/.hidane/memory/MEMORY.md`（带日期与 id 注释）
- `memory.candidate` 与 `memory.promoted` 事件落日志
- 之后的新 `chat` 提问相关话题时，Primary 的回答引用了该偏好（跨进程召回）
- 召回里的日期是本地日期（晚上说的话不会被说成前一天）
- 蒸馏若写进了某个工作项的 `MEMORY.md`：`hidane memories --ids`、`GET /api/memories`（`workItems`）与
  「记忆」页都能看到它；`hidane forget <id>` 或 `DELETE /api/memories/:id` 能删掉它，并落 `memory.forgotten`（带 `workItemId`）；
  `hidane forget` 一个不存在的 id 以非零状态退出

## 场景 4C：飞书连接器（长连接，无公开回调）

桌面应用没有公网地址，飞书入站改为官方 Go SDK 的长连接：连接用 App 凭证认证，不再有需要校验 token 的公开写口
（`POST /feishu/events` 的 404 与连接器的捕获、去重、分块、卡片格式由 `go test ./internal/api/ ./internal/feishu/` 守住）。期望：

- 若环境里有真实的 `FEISHU_APP_ID`/`FEISHU_APP_SECRET`（或 settings.json 的 `feishu` 段），启动 serve 后日志
  出现 `feishu channel enabled`，给机器人发一条单聊消息能在主线程看到 `connector.feishu` 与回复；没有凭证则 BLOCKED

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
  **一条**包含变更数量的确认回复（系统按实际结果写的确认，不再加上模型自己的那句）。
  该操作不是删除，也不应启动 Manager/Worker。用主会话让 Primary 停止一个运行中的任务同理：只有一条确认；
  同一句话里既停止又关闭（「停掉正在跑的并把所有任务关掉」）也只有一条确认，内容同时说明两件事。
- 通过主会话请求将一个已完成或已归档的工作项重新打开时，Primary 应使用其真实 ID
  将状态改回 open；请求“所有已有工作项”时可用批量状态操作，不能凭空编造 ID。

## 场景 4G：Web 通道也能发图给多模态模型

`POST /api/chat` 接受 `{"text": "...", "images": [{"data": "<base64>", "mimeType": "image/png"}]}`。
期望（需在设置里把 primary 角色指向一个能看图的模型，例如 claude 自带登录的默认模型）：

- 带图请求返回 202，`user.message` 事件 payload 带 `imageCount`
- 模型**真的看到了图**：自己构造一张内容明确的图（例如中间一个洋红色方块），
  问它"图里是什么颜色的形状"，回复必须与你画的内容相符，而不是"我没有收到图片"
- 只有图片、没有文字时同样接受（202），文字与图片都没有时返回 400
- 非图片 mimeType、超过 6MB、超过 4 张的部分被丢弃而不是让整条请求失败

## 场景 4I：长回复不截断、执行可中止、记忆可手写

- **长回复**：`ChunkText`（`internal/feishu`）把超长文本按段落切块而非截断。构造一段 >8000 字的文本，
  确认切块后**每块不超上限、拼回来内容不丢**（真实事故：8000 字回复在飞书被 `slice(0,4000)`
  砍掉一半，用户读到半截以为系统卡死，在执行早已成功 80 分钟后问「你是不是卡住了？」）
  回复本身也不被截断：Manager 的 >8000 字回复（以及 worker 的长总结）原样落进 `agent.reply`；
  只有超过 20 万字的极端情况才截，并且文末注明省略了多少字
- 模型输出不是效果列表（例如只回了一句「response.」或 `<reasoning_effort>5</reasoning_effort>`）时，
  Primary 与 Manager 都不把它当回复发给人：先自动追问一次（追问单独记一条 `route.decision` /
  `manager.decision`，带 `nudged: true`），追问仍拿不到效果列表才把原文作为回复
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

## 场景 4N：并发事件流不会返回空 body

SSE 曾在并发下出现「200 头 + 空 body」——首次写入前先查库，查询失败时响应已提交。
期望：

- 同时开 8 条 `/api/events/stream` 连接，**每条**都能读到首个 `hello` 事件（无空 body）
- 期间正常发一条 chat，各连接都能收到新事件
- 全部断开后服务仍健康（`/health` ok）

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
- 主会话只保留最多 320 条事件的连续窗口。向旧处翻页会释放较新的页，向新处翻页会释放较旧的页；
  两端仍可通过游标重新加载，搜索和永久链接能打开已经释放的消息。释放时应保持当前可见消息的位置；
  阅读历史时新回复不能挤走当前窗口，「回到最新」仍能回到实时对话。
- 流式回复的持久化副本已显示后，临时文本应立即释放；未打开任务详情的线程在持久化回复到达时释放，
  关闭详情页也应释放已经结束的临时文本，不必等待该线程的下一条回复。
- 长回复按段输出时，后端只解析新到达的内容；界面流式阶段逐字显示安全的纯文本，结束后按 GFM
  渲染完整 Markdown（包括表格），仍须过滤不安全的 HTML。不得在每段输出时重新解析累计全文。

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
  转给工作项 Manager 的那份副本、以及执行中转给 worker 的 `execution.steered` 原话同样被遮住。
  隐藏不追溯 agent 已经读到后**自己写出**的内容（工作项标题、brief、执行指令、理解）——这与
  「Manager 已经读到的不召回」一致；验收时把它们当作已知边界记录，而不是判失败
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
- 用真实 CLI 当 worker 时（`claude`、`codex`、`pi` 各一次），补充都并进**正在运行的这一轮**：这项工作只有一个
  `execution.started` / `execution.finished`，补充要求的产物出自这次执行，没有另派 worker，也不是等这一轮做完才另起一轮返工；
  补充之后执行照常结束（不会挂到超时）
- 在 Manager 规划期间（尚未派出 worker）到达的几句话，下一个 turn 一次取走：
  只有一个 `manager.decision` 的 `of` 同时包含这几条消息；若下一个 turn 时规划派出的 worker 已经在跑，
  这几句直接转给它（`execution.steered`，不再有新的 `manager.decision`）；刚派出、执行还在排队时到达的，记为
  `execution.steered`（`queued: true`）并写进 worker 的指令
- 任何情况下都**不**出现「could not deliver」之类的 `agent.error`：执行正要结束、送不进去的话记为
  `execution.steered`（`late: true`），随该执行的结果一起交给 Manager，Manager 的下一次回复要处理它

## 场景 5C：归属有歧义时不瞎猜，可改派

先建两个相似的工作项（如「做 login.html」「做 register.html」），再说一句含糊的话（「按钮颜色再深一点」）。期望：

- Primary 产出 `attribution.ambiguous`（带候选项），**不**把消息投给任何一个 Manager
- 通过 `POST /api/messages/:id/route {workItemId}` 选定后，落 `message.attributed`（by=user）并投递给对应 Manager
- 再改派到另一个工作项：新的 `message.attributed` 带 `previous`，原事件保留不改（只追加）
- 改派到 `new` 会新建工作项并投递
- 界面上：用户消息下方显示归属标签，歧义时显示候选按钮，不出现重复的问题气泡

## 场景 5D：问题逐层上报到人

提一个缺信息就做不了的任务（如「把 register.html 部署到我的服务器」，不给地址）。期望：

- Manager 不会静默停住：要么派 worker，要么回复，要么上报；只写「当前理解」的 turn 会被自动追问一次，
  追问的结果单独记一条 `manager.decision`（`nudged: true`）
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
- 不写 `tools` 的规则只管修改：只读命令（`ls`、`cat`、`grep` 及其 `;`/`&&`/`|`/换行组合、`if`/`for` 结构里的只读命令）
  提到被禁的文件不会被拦
- `policy.blocked` 在拦下的那一刻落日志（排在被拦调用的 `side_effect.result` 之前），任务还在跑时卡片上就能看到
- 结束后删除你加的规则

## 场景 5F：扇出成子任务，取消沿树向下

让一个任务拆成几个互相独立的部分（「分别调研 A、B、C，最后给对比表」）。期望：

- Manager 用子工作项扇出（`work_items.parent_id` 指向父项），每个子项有自己的工作区与 Manager
- 子项完成后 `done`；全部结束时父项收到一条 `children.settled`（含每个子项的结果），随后给出汇总
- 模型给出缺标题或缺说明的子项时不会静默丢弃：落一条 `agent.error`，写明是第几个子项、它的说明摘要和缺了什么；
  这条错误会出现在该 Manager 之后每个 turn 的历史里
- `children.settled` 在子项的最终回复**之后**落日志，结果就是那条最终回复；超长时注明截断，
  并指出完整内容所在的工作项与目录
- 子项的回复带 `payload.child = true`，不在主对话里出现；父项的汇总以最初的消息为 root
- 看板上父项在子项运行时为 `delegated`
- 另起一次扇出，在子项运行时 `POST /api/work-items/<父项>/cancel`：每个仍在执行的子项都落
  `execution.cancelled`（payload.via 为父项 id），随后各自的 `execution.finished` 带 `cancelled: true`；
  仍开着的子项被关闭（`closed`），父项**不**收到 `children.settled`，取消之后不再派出任何新的执行
  （人已经叫停了，Manager 不能把它当成「结果缺失」去补跑）

## 场景 5G：重启不丢事

让一个长执行跑起来，然后 `kill -9` 正在跑的 `hidane serve`（连同 agent CLI 子进程）再启动。期望：

- 启动日志写明「N execution(s) lost to the last restart」
- 该执行在 `executions` 表里为 `lost`，并有一条 `execution.finished`（`lost: true`）投递给其 Manager
- Manager 据此决定重试或说明情况——不会有一个永远「运行中」、无人跟进的执行
- 重启前积压在收件箱里的消息（游标之后的事件）在重启后被处理

## 场景 5H：因果链有上限

把 `HIDANE_MAX_HOPS` 调小（如 3）启动 `hidane serve`，让一个会多轮执行的任务跑起来。期望：

- 超过上限时消息不再投递，改为主线程上的一条 `escalation`（`reason: budget`）
- 同理 `HIDANE_MAX_EXECUTIONS_PER_ITEM` 用尽后不再派 worker，而是上报；这个上限按「自上次有人回复这个工作项以来」计数
- 人回复该工作项（或回答那条上报）后可以继续：跳数从 0 开始，执行次数重新计算，确实会再派出 worker
- 被预算拦下的那个 turn 里，Manager 预先写好的「已派出 worker」之类回复**不得**发给人

## 场景 5I：界面（需真实浏览器）

用 Playwright（chromium 与 webkit）打开 `hidane serve` 的界面。桌面形态可拦截 `/boot.js` 返回
`window.hidaneBoot = {desktop:true, auth:false, version:"acc"}`（token 仍放在 localStorage，请求照常鉴权；
`/wails/runtime.js` 不存在时实时通道自动回退到 SSE）——两种形态都要看。界面交互应是桌面应用的，不是网页的。验证：

- 布局：左侧边栏（新任务、搜索、会话/任务/定时/记忆/工作日志、「进行中」任务列表、底部设置齿轮），每页顶部 52px
  工具栏；⌘B 收起侧边栏并在刷新后保持；桌面形态下收起时工具栏左侧给红绿灯留空；整个窗口不滚动，只有内容区滚动
- 设置是替换整个主布局的独立界面（不是弹窗、不在主导航里）：⌘, / 侧边栏齿轮打开，Esc / 再按 ⌘, / 「← 返回」都回到
  进入前的那一页；分区：通用、快捷键、关于、角色、模型服务、Agent CLI、安全规则、运行状态、事件日志；
  旧地址 `/settings`、`/policies`、`/status`、`/events` 会跳到对应分区
- 单项设置改了就生效（语言、开关、角色的下拉；模型名与 CLI 路径失焦或回车保存，行内显示保存中/已保存/错误）；
  不兼容的角色组合留在屏幕上、不保存并给出警告；模型服务表单与新规则用显式保存
- 破坏性操作（停止任务、隐藏消息、删除模型服务/规则/记忆/定时、归档）都弹应用内确认框：取消不发生任何事，确认才执行；
  页面里不存在原生 `confirm/alert/prompt`
- ⌘K 命令面板：顶部居中浮层，列出命令（跳转页面、新任务、各设置分区）与全部历史中的对话/任务搜索结果，
  ↑↓ 回车 Esc 可用；从搜索结果打开一条对话会定位到以它为中心的历史（`?at=`），回到最新后地址里的 `?at=` 被清掉
- ⌘N 新任务对话框能直接建工作项并打开它；⌘L 聚焦输入框
- 右键菜单与「⋯」按钮给出同一组操作：消息（复制文本、隐藏；浏览器形态额外有复制链接），任务卡片与侧边栏任务
  （打开、停止、标记完成、归档；桌面形态额外有在访达中显示工作区）
- 对话流：归属标签（自动判断/你指定的/按当前聚焦 · 改）；新建的任务卡片在原消息下原位更新；迟到的回复出现在它所回答的
  消息下面；停在底部时新回复到来视图保持贴底（不会差几十像素、不会被「回到最新」或「有新回复」盖住）；
  往上翻时出现「回到最新」；侧边栏「进行中」列出运行中/等你回答/有新进展的任务，点开在会话旁打开聚焦面板
- 图片可拖进会话作为下一条消息的附件；在会话区外放下文件不会让窗口跳转
- 预算/截止类上报按界面语言显示，且都带 `payload.root`（截止上报指向开启该工作项的那条消息）；中英文切换后界面文案全部翻译（任务标题等内容保持原文）；`1 tool call` 单复数正确
- 浏览器形态有 token 门与退出；桌面形态没有，且 `/api/desktop/*` 在 serve 下 404 不会弹错误提示

## 场景 4：事件不灭与重放

期望：

- 事件被消费后依然留在日志里（append-only）
- 把某个消费者的游标重置后，能重新读到历史事件（重放语义）。
  可直接用 sqlite3 操作 cursors 表验证，注意别破坏 triage 消费者的现网状态（可用一个临时消费者名验证）。
- 重放蒸馏器（重置它的游标再 `hidane distill`）不会把已有的记忆再晋升一遍：同一条记忆不出现两个 id，
  模型换个说法也不行（全局与相关工作项的已有记忆都作为「不要重复」交给蒸馏器）；`distill.run` 的
  `promoted` 只计真正新增的条数


## 场景 7A：第一次提到一个仓库，任务在自己的工作树里做

在 /tmp 下建一个 git 仓库（有一次提交，默认分支 main），仓库根放一个 `hidane.json`：
`{"worktree":{"setup":"echo ready > .setup-done","teardown":"echo bye > $HIDANE_SOURCE_CHECKOUT_PATH/torn-down"}}`
（并把 `.setup-done` 写进 `.gitignore` 提交）。在对话里给出它的绝对路径，让它做一个小改动（例如加一个
`CHANGELOG.md`）。期望：

- 仓库被登记（`repo.registered`，名字是目录名），`GET /api/repos` 能看到
- 任务的工作区里多了一个工作树 `workspaces/<wi>/<仓库名>`，分支 `hidane/<wi>`，从 main 拉出
  （`checkout.created`），`.setup-done` 存在：setup 在第一个 worker 之前跑过，并作为这次执行的副作用记录
  （`side_effect.intent`，`tool` 为 `setup`）
- 改动只出现在工作树里；你自己的仓库目录 `git status` 干净，没有多出文件
- 主会话里的任务卡片标出了仓库名和分支
- 之后只说仓库名（不给路径）再提一个新任务，能找到同一个仓库，并且开的是**另一个**工作树、另一个分支

## 场景 7B：仓库名对不上唯一一个时先问，不开工

在 /tmp 下两个不同目录各建一个都叫 `blog` 的 git 仓库，把两个路径都登记上（对话里分别给出，或 `POST /api/repos`）。
然后只说「给 blog 加一个 RSS 页面」。期望：

- 助手先问是哪一个（回复里能看出两个候选，或者明确在问），**没有**创建任何工作项、没有开工作树
- 你回答之后（例如「第二个」或给出路径），才创建任务，工作树来自你选的那个仓库

## 场景 7C：迭代沿用原来的工作树，新任务另开

接着 7A 的任务（让它先完成并提交一次）。期望：

- 隔了好几条别的消息之后，再对那个任务提一个迭代（「刚才的 CHANGELOG 再加一行」）：路由回原任务，改动出现在
  **原来的**工作树里，没有新建工作树；即使那个任务已被标记为 done，只要工作树还在，也能接着做（任务被重新打开）
- 同时对同一个仓库提一个明显无关的新任务：它开了自己的工作树，两个任务互不影响
- 问「那个任务的分支里 CHANGELOG 写了什么」：在那个任务的工作树里读，回答与该分支内容一致

## 场景 7D：工作树由人管理，归档删目录、留分支

打开界面「任务 → 工作树」（`/items?view=worktrees`）。期望：

- 列出每个工作树：所属任务、仓库、分支、状态，以及领先主干的提交数和未提交改动数；能直接进入对应任务的会话
- 任务完成后工作树不会被自动清理
- 归档：先确认；若有未提交改动，再单独确认一次（API 未带 `force` 时返回 409 和 `dirty` 数）；任务正在运行时拒绝（409）
- 归档后：teardown 跑过（`torn-down` 文件出现在原仓库目录），工作树目录被删除，分支 `hidane/<wi>` 仍在原仓库里，
  记一条 `checkout.archived`；勾选「含已归档」仍能看到它
- 之后要接着那个已归档的任务做（例如「接着那个已归档的任务做」）：从它的分支继续——新的工作树从那个分支拉出
  （新任务开新分支，或原任务重新挂回自己的分支，都可以），包含之前的提交；新任务的卡片上标出它接着的是哪个分支
  （「接着 hidane/<原任务>」），工作树列表里同样能看出来
- 你归档了某个任务的工作树之后，它的 Manager 不会自己把工作树挂回来；只有你要求接着在那个仓库上做时才会

## 场景 7E：仓库被挪走或删掉，先感知、记录、问人

把一个已登记的仓库目录改名（模拟挪走）。期望：

- 重启 runtime、打开工作树页，或再让它在这个仓库上做事时，都能发现：记一次 `repo.missing`（不重复记），
  主会话里出现一条「仓库检查」提问，说明仓库不在原路径了
- 对这个仓库提新任务时不开工，而是问你仓库去哪了
- 告诉它新路径后，仓库保持原来的 id（`repo.relocated`），挂在它上面的已有工作树恢复可用（在工作树里 `git status` 正常）
- 说「那个仓库不要了」或在页面上移除：仓库从列表消失（`repo.forgotten`），磁盘上的任何文件都没被删

## 场景 7F：只有明确要求才在原目录上改

期望：

- 不特别说明时，新任务一律在新工作树里做；即使指令里写了原目录的路径，任务也改不了你的原目录（被拦下，提示去工作树里改）
- 明确说「直接在我的 xxx 目录（主干）上改」时，任务的检出是原目录（`mode: in_place`），改动出现在你自己的目录里，
  闸门放行这些写入；不会自己提交
- 另一个任务也要求在同一个原目录上改时，被拒绝并询问（等它结束，还是改用新工作树）
- 归档原目录模式的检出只是交还目录，原目录里的任何文件都不会被删除

## 场景 7G：一个任务跨多个仓库，子任务继承仓库

期望：

- 「后端 api 加一个字段，前端 web 跟着展示」（两个已登记的仓库）：一个任务挂上两个工作树，卡片列出两个仓库与各自分支，
  worker 两边都改了
- 一个在仓库上工作的任务被拆成子任务时，每个子任务有自己的工作树，分支从父任务的分支拉出；全部完成时父任务收到的
  消息里写明每个子任务的分支，父任务能把它们合并回自己的工作树

## 场景 6A：本机 agent CLI 与 LLM Provider 配置

- `hidane agents` 列出 claude / codex / pi 的可用性、版本与路径；在 `settings.json` 的 `binaries` 里指向
  `bin/fake/*` 后，`/api/agents` 显示这三个假 CLI 可用
- `POST /api/providers` 新建一个 provider（例如 DeepSeek 预设：Anthropic 兼容地址 + pi provider 名 + key）：
  响应与 `GET /api/settings` 里**只有** `hasApiKey` 与末四位提示，任何响应、事件（`settings.updated`）都不含 key；
  `settings.json` 权限为 0600 且确实存了 key
- 把 codex 角色指向一个没有 OpenAI Responses 地址的 provider → 400 且原配置不变；claude 需要 Anthropic 兼容地址、
  pi 需要 pi provider 名，同理
- 被角色使用中的 provider 不能删除（409，错误里点名角色）
- 角色指向真实 CLI 时 `hidane model --ping --role <角色>` 返回 `ping ok`（花费少量 token）；指向假 CLI 同样 ok
- 注入方式核对（可用假 CLI 的 `FAKEAGENT_LOG` 记录或 `ps` 观察）：claude 拿到 `ANTHROPIC_BASE_URL`/`ANTHROPIC_AUTH_TOKEN`
  环境变量；codex 拿到 `-c model_provider=hidane …` 与 `HIDANE_CODEX_API_KEY` 环境变量；pi 拿到 `--provider`。
  key 不出现在 claude/codex 的命令行参数里

## 场景 6E：对话里选择 agent、模型和推理强度

像 Paseo 一样，对话输入框下方有两个入口，各是一个按钮：「对话」显示并切换由谁回复对话（Primary 角色的设置），
「任务」选新任务（或聚焦的那个任务）用哪个 agent CLI、模型服务、模型和推理强度。点开是一个浮层（手机宽度下是底部面板），
里面先是「收藏的组合」，再是 agent、模型服务、模型、推理强度。期望：

- 选项来自 CLI 本身：Codex 的模型和每个模型支持的推理强度来自 `codex debug models`，pi 的模型来自
  `pi --list-models`，Claude Code 用内置列表；模型可以直接输入列表里没有的名字；推理强度只列出所选
  CLI/模型接受的档位（claude 低…最高，codex 最低…极致，pi 不思考…最高）
- 「对话」按钮显示的是 Primary 角色当前的设置；在浮层里换 agent/模型/强度会立刻保存到 `roles.primary`
  （有「之后的对话由 … 回复」的提示，落一条 `settings.updated`），其他角色不变；下一条闲聊就由新选的 CLI 回答，
  之前的对话照样在上下文里（会话目录或 CLI 调用里能看到这一轮用的是新 CLI）；浮层底部「全部角色设置」打开设置的角色分区
- 「收藏当前组合」把 agent + 模型服务 + 模型 + 推理强度整套存下；点一个收藏一次切换整套（对话或任务都可以），
  再点一次星标或 × 取消收藏；收藏和为新任务记住的选择在刷新页面后仍在；当前不可用的组合（CLI 未检测到、模型服务已删或不兼容）
  显示但不能点，并说明原因
- 在主会话里用「任务」选 Codex + 某个模型 + 推理强度后提一个要动手的任务（真实 CLI）：新建的工作项带 `runAs`，
  落一条 `work_item.run_as_changed`；它的 Manager 与 worker 真的用 codex 跑（会话目录里的 manager
  session 记的是 codex，worker 的命令参数里有所选模型与 `model_reasoning_effort`），而角色设置仍是原样；
  任务卡片上显示「Codex · 模型 · 强度」
- 聚焦这个任务时，「对话」按钮隐去（这条消息由任务自己的 Manager 回答），「任务」浮层显示它自己的设置并注明
  「用于这个任务，立即生效」；改成 pi 立刻生效（`PATCH` 成功、有提示、再落一条 `work_item.run_as_changed`），
  改回「跟随设置」后 `runAs` 为 null；连续快速改模型和强度，最后保存的是最后一次的完整组合
- 子任务继承父任务的选择
- 非法组合被拒（400）：未知 agent、该 CLI 不支持的推理强度、与 CLI 不兼容的模型服务、带空格的模型名
- `hidane chat --agent codex --effort high "…"` 在命令行做同样的事；`--model`/`--effort` 不带 `--agent` 时报错

## 场景 6B：闸门在真实 CLI 里生效

对 worker 分别用 claude、codex、pi（真实 CLI）各跑一次：全局 `POLICY.json` 加一条 `forbidden\.txt` 规则，
让任务「创建 hello.txt 与 forbidden.txt」。期望：

- 三次都只有 `workspaces/<wi>/hello.txt`；`forbidden.txt` 不存在；各有 `policy.blocked`（rule 为该规则 id）
- 闸门只拦内置的高危命令（`sudo`、`rm -rf /`、强制 push 等）和你的规则；工作区只是起点和默认产物位置，不是围栏：
  写到工作区外（`/tmp`、`~` 下其他目录）都放行。可直接用 `hidane guard --format claude` 喂入
  `{"tool_name":"Bash","tool_input":{"command":"sudo ls"}}`（拒绝）与 `"echo x > /tmp/y"`（放行）验证——
  验证时只用无害的命令，不要真的执行高危操作
- 只读的命令不被规则误拦：引号里的 `>`、`;` 是文字不是语法，用 `;`/`&&`/`|`/换行串起来、或写在 `if … fi` 里的只读命令仍算只读
- 把 `POLICY.json` 改成非法 JSON 后再派一次写操作：被拒（「policy file … is unreadable」），而不是规则静默失效
- 最终回复如实说明哪个成功、哪个被策略阻止

## 场景 6C：桌面应用

- `make smoke-gui`：真实的 Wails 窗口加载内嵌界面，界面经 Wails 事件（而不是 SSE 回退）收到实时帧后自动退出 0；
  输出含 `ui ready (live transport: wails)`；`make app` 产出的 `bin/Hidane.app` 同样通过
  （`HIDANE_GUI_SMOKE=1 bin/Hidane.app/Contents/MacOS/hidane`）
- 原生菜单（hidane / File / Edit / View / Window）存在且带快捷键：Settings… ⌘,、New Task… ⌘N、New Message ⌘L、
  会话/任务/定时/记忆/日志 ⌘1–⌘5、Search ⌘K、Toggle Sidebar ⌘B——可用 `osascript` 读取应用菜单栏验证（若无辅助功能权限则 BLOCKED）
- 隐藏式标题栏：窗口大小与位置在移动/缩放后写入 `$HIDANE_HOME/runtime/window.json`，下次启动恢复
- 桌面专用端点（`/api/desktop/open-url`、`clipboard`、`notify`、`badge`、`open-data-dir`、`/api/work-items/:id/reveal`）
  在 `hidane serve` 下一律 404；`open-url` 只接受 http(s)/mailto
- 再次打开不会起第二个实例；桌面模式下 `/boot.js` 为 `desktop: true, auth: false`：不出现 token 输入框
