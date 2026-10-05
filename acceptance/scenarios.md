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
  `?token=` 链接；只有 SSE（`/api/events/stream`，EventSource 不能带请求头）也接受 `?token=`，文件下载等其他
  `/api/*` 只认请求头（网页的下载按钮带请求头取文件，由 e2e 守住）。同一 HIDANE_HOME 只允许一个 runtime（文件锁）
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
- worker 真的在工作区产出了文件，且文件内容与任务相符
- 最终回复内容与实际产物一致（不是编造的）

## 场景 1B：小事 Primary 自己动手

Primary 和 Manager 都带工具（各 CLI 的 bypass 模式，只受闸门的高危命令与规则约束）。期望：

- 问一个看一眼就能答的问题（例如「/tmp 下某个你刚建的文件里写了什么」）：Primary 自己用工具查完直接回答，
  不开工作项；回答与文件内容一致
- 明显要多步完成、值得跟进的活，仍然开工作项交给 worker
- 要它建文件或改文件、却没给绝对路径（例如「在工作区里创建 a.txt…」）：再小也开工作项，文件在
  `workspaces/<wi>/` 下，`runtime/roles/primary/` 里没有它产出的文件

## 场景 2：外部事件由 Primary 回应

用 `hidane serve`（设置 `HIDANE_WEBHOOK_SECRET`）启动，向 webhook 端点投递一条带正确签名、内容明确的事件。期望：

- primary 对该外部事件的回复（`agent.reply`，`payload.rootKind = external`）内容与事件相关，不是乱答
- 结束后清理你启动的后台进程

## 场景 4B：记忆蒸馏与跨日召回

用 `chat` 告诉 Primary 一条明确的、此前不存在的长期偏好（编一条具体的），然后 `hidane distill --min 1`。期望：

- 偏好被提取并晋升进 `~/.hidane/memory/MEMORY.md`
- 之后的新 `chat` 提问相关话题时，Primary 的回答引用了该偏好（跨进程召回）

## 场景 4C：飞书连接器（长连接，无公开回调）

桌面应用没有公网地址，飞书入站改为官方 Go SDK 的长连接：连接用 App 凭证认证，不再有需要校验 token 的公开写口
（`POST /feishu/events` 的 404 与连接器的捕获、去重、分块、卡片格式由 `go test ./internal/api/ ./internal/feishu/` 守住）。期望：

- 若环境里有真实的 `FEISHU_APP_ID`/`FEISHU_APP_SECRET`（或 settings.json 的 `feishu` 段），启动 serve 后日志
  出现 `feishu channel enabled`，给机器人发一条单聊消息能在主线程看到 `connector.feishu` 与回复；没有凭证则 BLOCKED

## 场景 4F：工作项状态可改

- 通过主会话 `chat` 请求“把当前所有打开的工作项关停/关闭”时，Primary 应直接执行
  批量状态变更，不再回复“没有相应能力”；当时所有 open 工作项均变为 closed，
  每个变更都有 `work_item.status_changed`（source 为 `agent:primary`）。
  该操作不是删除，也不应启动 Manager/Worker。用主会话让 Primary 停止一个运行中的任务、或同一句话里既停止又关闭
  （「停掉正在跑的并把所有任务关掉」），同样直接执行。
- 通过主会话请求将一个已完成或已归档的工作项重新打开时，Primary 应使用其真实 ID
  将状态改回 open；请求“所有已有工作项”时可用批量状态操作，不能凭空编造 ID。

## 场景 4G：Web 通道也能发图给多模态模型

`POST /api/chat` 接受 `{"text": "...", "images": [{"data": "<base64>", "mimeType": "image/png"}]}`。
期望（需在设置里把 primary 角色指向一个能看图的模型，例如 claude 自带登录的默认模型）：

- 模型**真的看到了图**：自己构造一张内容明确的图（例如中间一个洋红色方块），
  问它"图里是什么颜色的形状"，回复必须与你画的内容相符，而不是"我没有收到图片"

## 场景 4I：执行可中止

- **中止执行**：让一个工作项在真实 CLI 上跑起来（要求它做几十次工具调用），在执行中
  `POST /api/work-items/:id/cancel`。期望：执行**数秒内**结束，`execution.finished` 带 `cancelled: true` / `ok: false`
  ——注意 `abort()` 会让 agent 自然 idle，若按「谁先完成」判定会把被中止的执行错记为成功，结果标签必须跟随用户意图。
- 也应能在主会话中说“停止这个正在运行的工作项”；Primary 直接发出同样的取消意图，
  不应把停止请求转成新的 Manager/Worker 执行。

> **清理约定**：任何写入 MEMORY.md 的场景（4B）在结束前必须把自己写的记忆
> `forget` 掉。留下的记忆会注入后续每一次路由——曾有一条「回复开场白永远用『好的』」
> 的验收残留，让之后所有回复都以「好的」开头。

## 场景 4K：先记下、暂不开始的工作项

- 通过主会话说“先记录这个工作项，暂时不要开始”时，Primary 应创建 open 工作项并记录
  deferred 消息，但不启动 Manager/Worker；后续明确要求开始时仍可正常路由。

## 场景 4R：Primary 的上下文有界，更早的内容靠检索

Primary 不再依赖一个无限增长的模型会话：每个 turn 新开会话，从日志重建「最近的对话」。期望：

- `GET /api/conversation/context` 给出 Primary 当前视野的起点 `fromId`（最近若干轮，有条数与字数上限，
  不含被隐藏的消息）；界面在这条消息上方标出「助手现在只参考从这里往下的对话」
- 每个 turn 在会话目录留一个独立的 trace 文件；重启前后 Primary 的行为一致（上下文来自日志，而非进程内存）
- 问一件远在视野之外的旧事（例如很早之前某个工作项里的具体字符串）：Primary 先产出 `recall`
  效果并**立即结束这个 turn**（不等待），随后一条 `conversation.recalled`（投给 `primary` 收件箱，
  带 `query` 与检索结果）触发下一个 turn，回答挂在最初那条消息下（`payload.root` 为原消息 id），内容与历史相符。
  对 recall 结果不会再次 recall

## 场景 5B：分钟级补充被合并，而不是各自返工

对同一个工作项在它执行期间连续补充两句（直接对工作项说：`POST /api/chat` 带 `target`）。期望：

- 最终产物体现了补充的要求（例如补充「支持 --dry-run」，脚本里就有这个参数）
- 用真实 CLI 当 worker 时（`claude`、`codex`、`pi` 各一次），补充都并进**正在运行的这一轮**：这项工作只有一个
  `execution.started` / `execution.finished`，补充要求的产物出自这次执行，没有另派 worker，也不是等这一轮做完才另起一轮返工；
  补充之后执行照常结束（不会挂到超时）

## 场景 5C：归属有歧义时不瞎猜，可改派

先建两个相似的工作项（如「做 login.html」「做 register.html」），再说一句含糊的话（「按钮颜色再深一点」）。期望：

- Primary 产出 `attribution.ambiguous`（带候选项），**不**把消息投给任何一个 Manager
- 通过 `POST /api/messages/:id/route {workItemId}` 选定后，落 `message.attributed`（by=user）并投递给对应 Manager
- 等这个 Manager 按这句话动了手（派了 worker），再把消息改派到另一个工作项：原 Manager 之后的 turn（例如读到那次
  worker 结果时）不再为这句话继续做——不为它再派 worker、不就它追问；新工作项的 Manager 接手去做
- 改派到 `new` 会新建工作项并投递
- 界面上：用户消息下方显示归属标签，歧义时显示候选按钮，不出现重复的问题气泡

## 场景 5D：问题逐层上报到人

提一个缺信息就做不了的任务（如「把 register.html 部署到我的服务器」，不给地址）。期望：

- Manager 不会静默停住：要么派 worker，要么回复，要么上报；缺地址这类信息时上报问人，问题问到点子上
- 用 `POST /api/chat {replyTo: <escalation id>}` 回答后，任务用上你的回答继续推进
- 缺的是一个选择时（例如「部署到 staging 还是 production」），上报带 `options`（2–5 个短答案）；
  缺的是开放的信息（地址、密钥）时不带 `options`

## 场景 5J：一句话发给几个任务，答过的问题转成任务

先让两个互不相关的工作项各跑完一次（例如「写 notes-a.md，内容随意」「写 notes-b.md，内容随意」），再用**一条**消息
同时发给这两个（界面里用 `@` 选两个，或 `POST /api/chat {"targets":[a,b]}`，或 `hidane chat --item a,b`），话里
给两边不同的要求（「a 里加一节安装说明；b 全部改成英文」）。期望：

- 各自只做属于自己的部分：notes-a.md 有了安装说明、没被改成英文；notes-b.md 改成了英文、没有安装说明；两边都没有把
  这条消息 reroute 回 Primary
- 对 Primary 直接回答过的一个小问题调用 `POST /api/messages/:id/promote`：新建的工作项的 Manager 第一次决定用上了
  Primary 已有的回答（不是从零重复去查同一件事），也不把这个回答当成人说的话

## 场景 5F：扇出成子任务

让一个任务拆成几个互相独立的部分（「分别调研 A、B、C，最后给对比表」）。期望：

- Manager 用子工作项扇出（每个独立的部分一个子项），而不是交给一个 worker 全做
- 子项都完成后，父项给出的汇总（对比表）与各子项的结果相符

## 场景 5G：重启不丢事

让一个长执行跑起来，然后 `kill -9` 正在跑的 `hidane serve`（连同 agent CLI 子进程）再启动。期望：

- 启动日志写明「N execution(s) lost to the last restart」
- Manager 据此决定重试或说明情况——不会有一个永远「运行中」、无人跟进的执行

## 场景 5I：界面（需真实浏览器）

用 Playwright（chromium 与 webkit）打开 `hidane serve` 的界面。桌面形态可拦截 `/boot.js` 返回
`window.hidaneBoot = {desktop:true, auth:false, version:"acc"}`（token 仍放在 localStorage，请求照常鉴权；
`/wails/runtime.js` 不存在时实时通道自动回退到 SSE）——两种形态都要看。界面交互应是桌面应用的，不是网页的。验证：

- 布局：左侧边栏（新任务、搜索、会话/待处理/任务/定时/记忆/工作日志、「待处理」与「进行中」两组任务、底部设置齿轮），每页顶部 52px
  工具栏；桌面形态下侧边栏收起时工具栏左侧给红绿灯留空
- 设置的分区：通用、快捷键、关于、角色、模型服务、Agent CLI、安全规则、运行状态、事件日志
- ⌘K 命令面板是顶部居中的浮层
- 对话流：用户消息下方的归属标签（自动判断/你指定的/按当前聚焦 · 改）；侧边栏「待处理」「进行中」里的任务点开后在会话旁打开聚焦面板；
  「待处理」页上待审阅的任务能展开看改动（逐文件差异）
- 在会话区外放下文件不会让窗口跳转
- 中英文切换后界面文案全部翻译（任务标题等内容保持原文）
- 浏览器形态有退出登录

## 场景 4：重放蒸馏不重复晋升

先有一条蒸馏出的记忆（例如 4B 的）。期望：

- 重放蒸馏器（重置它的游标再 `hidane distill`）时，真实模型不会把已有的记忆换个说法再晋升一遍：同一条记忆不出现两个 id
  （全局与相关工作项的已有记忆都作为「不要重复」交给蒸馏器）

## 场景 7A：第一次提到一个仓库，任务在自己的工作树里做

在 /tmp 下建一个 git 仓库（有一次提交，默认分支 main）。在对话里给出它的绝对路径，让它做一个小改动（例如加一个
`CHANGELOG.md`）。期望：

- 改动只出现在任务的工作树（`workspaces/<wi>/<仓库名>`）里；你自己的仓库目录 `git status` 干净，没有多出文件
- 之后只说仓库名（不给路径）再提一个新任务，能找到同一个仓库，并且开的是**另一个**工作树

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

## 场景 7D：归档之后，接着做由人决定

归档一个已提交过的任务的工作树（界面「任务 → 工作树」，或 `POST /api/checkouts/:id/archive`）。期望：

- 之后要接着那个已归档的任务做（例如「接着那个已归档的任务做」）：从它的分支继续——新的工作树从那个分支拉出
  （新任务开新分支，或原任务重新挂回自己的分支，都可以），包含之前的提交
- 你归档了某个任务的工作树之后，它的 Manager 不会自己把工作树挂回来；只有你要求接着在那个仓库上做时才会

## 场景 7E：仓库被挪走，在对话里修好或移除

把一个已登记、挂着工作树的仓库目录改名（模拟挪走），再让它在这个仓库上做事——它会问你仓库去哪了。期望：

- 在对话里告诉它新路径后，还是原来那个仓库（`repo.relocated`，不是新登记一个），已有的工作树恢复可用
- 在对话里说「那个仓库不要了」：仓库从列表消失（`repo.forgotten`），磁盘上的任何文件都没被删

## 场景 7F：只有明确要求才在原目录上改

期望：

- 不特别说明时，新任务一律在新工作树里做；即使你的话里写了原目录的路径，任务也只在工作树里改、不动你的原目录
  （闸门不围住任何目录，这靠模型守约定：没有你的要求不改原目录）
- 明确说「直接在我的 xxx 目录（主干）上改」时，任务的检出是原目录（`mode: in_place`），改动出现在你自己的目录里；
  不会自己提交

## 场景 7G：一个任务跨多个仓库，子任务继承仓库

期望：

- 「后端 api 加一个字段，前端 web 跟着展示」（两个已登记的仓库）：一个任务挂上两个工作树，worker 两边都改了
- 一个在仓库上工作的任务被拆成子任务时，子任务全部完成后，父任务能把它们的分支合并回自己的工作树

## 场景 6A：本机 agent CLI 与 LLM Provider 配置

- `hidane agents` 对本机真实安装的 claude / codex / pi 列出可用性、版本与路径
- 角色指向真实 CLI 时 `hidane model --ping --role <角色>` 返回 `ping ok`（花费少量 token）

## 场景 6E：对话里选择 agent、模型和推理强度

像 Paseo 一样，对话输入框下方有两个入口，各是一个按钮：「对话」显示并切换由谁回复对话（Primary 角色的设置），
「任务」选新任务（或聚焦的那个任务）用哪个 agent CLI、模型服务、模型和推理强度。点开是一个浮层（手机宽度下是底部面板），
里面先是「收藏的组合」，再是 agent、模型服务、模型、推理强度。期望：

- 选项来自本机真实 CLI：Codex 的模型和每个模型支持的推理强度来自 `codex debug models`，pi 的模型来自
  `pi --list-models`，与 CLI 自己列出的一致
- 在「对话」浮层把 Primary 换成另一个真实 CLI 后，下一条闲聊就由新选的 CLI 回答，之前的对话照样在上下文里
  （会话目录或 CLI 调用里能看到这一轮用的是新 CLI）
- 当前不可用的收藏组合（CLI 未检测到、模型服务已删或不兼容）显示但不能点，并说明原因
- 在主会话里用「任务」选 Codex + 某个模型 + 推理强度后提一个要动手的任务（真实 CLI）：它的 Manager 与 worker
  真的用 codex 跑（会话目录里的 manager session 记的是 codex，worker 的命令参数里有所选模型与 `model_reasoning_effort`）

## 场景 6B：闸门在真实 CLI 里生效

对 worker 分别用 claude、codex、pi（真实 CLI）各跑一次：全局 `POLICY.json` 加一条 `forbidden\.txt` 规则，
让任务「创建 hello.txt 与 forbidden.txt」。期望：

- 三次都只有 `workspaces/<wi>/hello.txt`；`forbidden.txt` 不存在；各有 `policy.blocked`（rule 为该规则 id）
- 真实 worker 查看文件时用的只读命令（不管怎么串：`;`/`&&`/`|`/换行、`if … fi`）不被规则误拦
- 最终回复如实说明哪个成功、哪个被策略阻止

## 场景 6C：桌面应用

- `make smoke-gui`：真实的 Wails 窗口加载内嵌界面，界面经 Wails 事件（而不是 SSE 回退）收到实时帧后自动退出 0；
  输出含 `ui ready (live transport: wails)`；`make app` 产出的 `bin/Hidane.app` 同样通过
  （`HIDANE_GUI_SMOKE=1 bin/Hidane.app/Contents/MacOS/hidane`）
- 原生菜单（hidane / File / Edit / View / Window）存在且带快捷键：Settings… ⌘,、New Task… ⌘N、New Message ⌘L、
  会话/任务/定时/记忆/日志 ⌘1–⌘5、Search ⌘K、Toggle Sidebar ⌘B——可用 `osascript` 读取应用菜单栏验证（若无辅助功能权限则 BLOCKED）
- 隐藏式标题栏：窗口大小与位置在移动/缩放后写入 `$HIDANE_HOME/runtime/window.json`，下次启动恢复
- 再次打开不会起第二个实例（换一个 `HIDANE_HOME` 再开时，在交给已运行实例之前不会在那个目录里创建任何东西，由 `go test ./internal/desktop/` 守住）
