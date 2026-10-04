# hidane：从 Node/Postgres Web 版到 Go + Wails v3 桌面客户端

> 改造已完成。本文只记录为什么这样改、存储怎么对应、闸门的边界，以及实施中由实测逼出来的决定；
> 目录结构、角色与 CLI 驱动、Provider 配置、验证命令见 `README.md` 与 `AGENTS.md`，以那里为准。

## 1. 为什么改、改成什么

原形态是「Node daemon + Postgres + 浏览器 SPA + Coolify 部署」，agent 通过进程内 pi SDK
调用模型。作为个人 agent 运行时，它真正需要的是：常驻本机、直接操作本机文件与仓库、复用
本机已登录的 coding agent CLI。因此改造为：

| 维度 | 改造前 | 改造后 |
|---|---|---|
| 宿主 | Node 24 daemon（Docker / Coolify） | 单个 Go 二进制：Wails v3 桌面应用 + 同一二进制的 CLI / headless `serve` 模式 |
| 存储 | Postgres（LISTEN/NOTIFY、advisory lock） | SQLite（WAL，纯 Go 驱动 `modernc.org/sqlite`）；进程内通知 hub；文件锁保证单 runtime |
| 前端 | Svelte 5 SPA（浏览器访问 daemon） | **继续使用 Svelte 5 SPA**，Vite 构建后 `go:embed` 进二进制，由 Wails 的 AssetServer Handler 提供 |
| 前后端通信 | fetch `/api/*` + SSE | 不变：同一个 `http.Handler` 同时服务 Wails webview 与 headless 浏览器；实时事件在桌面端走 Wails Events，在浏览器端走 SSE（同一个 Go hub 驱动） |
| Agent 调用 | 进程内 pi SDK（Primary/Manager）+ `pi --mode rpc` 子进程（Worker） | **全部改为本机 CLI 子进程**：`claude`（Claude Code）、`codex`、`pi`，按角色可选 |
| 模型配置 | 环境变量 `HIDANE_PI_PROVIDER/MODEL/API_KEY` | 应用内 **LLM Provider 配置**（设置页 + `settings.json`），按角色选择 agent CLI / provider / model / effort |
| 测试 | vitest（server + web）+ agent 驱动验收 | Go `go test`（内核不变量、驱动协议、API）+ 前端 vitest + Playwright E2E（真实 Go 后端 + 假 agent CLI）+ 桌面 GUI 冒烟 + 真实 CLI 冒烟 |

## 2. 存储：Postgres → SQLite

- 表结构一一对应：`events`（append-only，`seq INTEGER PRIMARY KEY AUTOINCREMENT`）、`cursors`、
  `threads`、`work_items`、`executions`、`schedules`、`channel_bindings`。payload 为 JSON 文本，
  查询用 `json_extract`。
- `ts` 存 UTC 毫秒 ISO 字符串，字典序即时间序。
- 隐藏消息（`message.redacted`）仍是**读时投影**：SQL 中用 `json_remove/json_set` 屏蔽内容，行不改。
- LISTEN/NOTIFY → 进程内 `Hub`（追加即广播 seq）；跨进程（CLI 与桌面应用同时运行）靠各自的
  兜底轮询，通知始终只是提示，消费者从自己的游标读日志。
- `pg_advisory_lock` → `HIDANE_HOME/runtime.lock` 文件锁：同一份数据只允许一个 runtime。
- 不变量保持：事件只追加；回放 = 重置游标；投影可重建；数据库只放需要原子有序的状态，
  agent 读的内容（记忆、工作日志、TASK.md、会话轨迹）仍然是文件。

## 3. 闸门（capture 阶段）统一实现

原 `extensions/pi-guard.ts` 的规则迁入 Go `internal/guard`：内置拒绝表（`sudo`、`rm -rf /`、
`mkfs`、`dd if=`、fork bomb、`shutdown/reboot`、`git push --force`）、全局→祖先工作区→本工作区的
`POLICY.json`、有待读输入时暂停写操作。三个 CLI 都通过
`hidane guard` 子命令接入同一份逻辑（claude/codex 用 hook 协议，pi 用一个极小的 TS 扩展转调），
规则只读文件、不调模型；被拦截的调用写入 `HIDANE_POLICY_BLOCKS_FILE`，执行结束时作为
`policy.blocked` 事件记入日志。

闸门读的是命令文本，所以有边界：`python3 -c` / `node -e` 这类脚本往哪写，文本里不一定看得出来，
闸门拦不住。后来工作区也不再是围栏：agent 在各 CLI 自己的 bypass 模式下运行，闸门只剩内置拒绝表、
策略文件和待读输入暂停（现状以 `AGENTS.md` 为准）。

## 4. 实施记录：与计划不同、由实测逼出来的决定

计划之外的改动都有来由——大多来自用本机真实 `claude` / `codex` / `pi` 跑任务，或来自一次独立的代码评审。
其中「推理角色不带工具」与「工作区禁锢」两条已被后来的改动取代：Primary 与 Manager 现在带工具运行
（charter 仍替换系统提示），工作区只是起点而不是围栏。

| 现象（证据） | 决定 |
|---|---|
| 真实 claude haiku 当 Primary 时，被告知自己是带文件工具的 Claude Code，直接回复「两个文件都创建成功了」，实际没有任何工作项、没有任何文件 | 推理角色（Primary / Manager / 蒸馏）用 charter **替换** CLI 的系统提示（claude/pi `--system-prompt`），只有 worker 追加；Primary charter 明确「你没有工具，凡是要做的事都建工作项，绝不声称做了没做的事」 |
| Primary 的 cwd 是 `HIDANE_HOME`，模型把它写进 brief，真实 pi worker 于是 `cd ~/.hidane && echo hi > hello.txt`——写进了存放 POLICY.json / settings.json（含 key）/ 数据库的目录 | 推理角色在 `runtime/roles/<role>/` 空目录里运行；闸门新增**工作区禁锢**：文件工具只能写本工作项工作区，shell 命令不得改动 hidane 数据目录，也不得改动工作区里的 `.hidane`（工作区自己的策略与轨迹） |
| 一个手写坏了的 POLICY.json（`\.` 非法转义）让全部规则静默失效 | 存在但读不懂的策略文件 → 拒绝一切修改性调用（fail closed）；缺失的文件仍等于「无规则」；设置页增删规则时同样报错，而不是用新规则覆盖掉手写的文件 |
| 真实 claude worker 把三步写成一条 shell 命令，整条被拒，Manager 却转述成「前两步已完成」 | 闸门拒绝时只对模型追加「这次调用里什么都没执行，请把允许的部分拆成单独调用」；日志里的 `policy.blocked` 原因保持干净。复测：worker 拆开执行，结论如实 |
| 评审：pi 会展开 `~`、去掉前导 `@`；「只读命令」白名单放过多行命令、`$(…)`、`find -delete`；codex 补丁的 `Move to:` 未检查 | 闸门按 CLI 的方式解析路径；多行 / 命令替换 / 带动作参数的命令一律视为修改；补丁的移动目标同样检查 |
| 评审：闸门自己的控制文件（待读输入标记、拦截记录）在 worker 可写的工作区里 | 移到 `runtime/executions/<id>/`，worker 删不掉也伪造不了 |
| 评审：退出应用时 worker CLI 仍在后台运行；下次启动又判它 lost 并派第二个 writer 进同一工作区 | 退出时停止所有 worker 并以 `lost` 报告给 owner；被关停打断的 turn 不提交游标，重启后重跑 |
| 评审：worker 结果可能被跳数预算吞掉（违反「每个执行的结果必达 owner」） | 预算在派发前检查（花钱之前）；`execution.finished` 永远投递 |
| 评审：蒸馏读窗口固定 200 条，安静期里会永远停住 | 向前扫描直到素材足够、到达日志头或上限 |
| 评审：serve 模式下未设 secret 的 webhook 任何网页都能跨域 POST 触发；任何能私聊机器人的飞书用户都能驱动 agent | 未设 `HIDANE_WEBHOOK_SECRET` 时 webhook 一律 403；飞书增加发送者白名单，未配置时只认第一个私聊（主人），其余只记录 `connector.feishu_ignored` |
| WKWebView 里点回复中的外链会把整个应用窗口导航走；下载没有落盘的地方 | 桌面模式拦截外链交给系统浏览器、产物「下载」改为在访达中显示（仅桌面端点，serve 不暴露） |
| 第二轮验收：真实 claude 把运行中追加的消息并进当前这一轮，只出一个 result；驱动按「每条消息一个 result」等待，执行一直挂到 10 分钟超时 | 一条消息被回放（`isReplay`）即视为已读入；`queued_turn_count == 0` 且所有消息都已读入/都有 result 才收尾。假 CLI 增加 `FAKEAGENT_CLAUDE_ABSORB` 模拟这种行为 |
| 第二轮验收：Manager 刚派出 worker 时追加的消息被报「无法送达」丢掉 | 送达看 worker 池自己的状态而不是 `executions` 表（任务离开队列早于表变成 running）；执行正要结束、送不进去的话记为 `execution.steered`（`late`），随这次执行的结果一起交给 Manager |
| 第二轮验收：子任务同一轮里「完成 + 回复」，`children.settled` 先于回复发出，父任务拿到旧结果；结果在 3000 字处被无声截断 | Manager 效果分三段执行：其它 → 回复 → `done`；结果上限 6000 字，截断处注明完整内容所在的工作项与目录 |
| 第二轮验收：工作项层的记忆（`<workspace>/MEMORY.md`）`forget` / `DELETE` 都找不到 | 遗忘按 id 在全局与所有工作项层里找；`GET /api/memories` 与「记忆」页、`hidane memories` 都列出工作项层 |
| 第二轮验收：`hidane chat` 在 Manager 最后一轮还没跑完时就退出 | 等整个系统安静 2 秒（无执行、无未读、无进行中的 turn）才退出 |
| 第二轮验收：Primary 停止任务 / 批量改状态时出现两条确认 | 系统对实际结果的确认替代模型事先写好的回复（回复在其它效果之后执行） |
| 第二轮验收：召回结果用 UTC 日期，模型把本地 10 月 2 日早上说的话说成 10 月 1 日 | 召回按本地时间写到分钟 |
| 第二轮验收：每次启动窗口小 1px | Wails 在 macOS 上按 `width-1, height-1` 建窗口，恢复时补回 |
| 桌面端 Wails 事件是推送的，页面订阅前的 `hello` 会丢 | 页面订阅后请求一次问候（`POST /api/live/hello`），GUI 冒烟要求实时帧确实经 Wails 事件到达 |
