# hidane → Go + Wails v3 桌面客户端改造方案

> 状态：执行中（本文件既是计划，也是改造完成后的架构说明）。
> 目标版本：Wails `v3.0.0-beta.27`（2026-10 时最新），Go 1.26，Svelte 5 + Vite。

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

**前端能否继续用 Svelte？能。** Wails v3 对前端框架无要求，只需要一个静态产物目录。现有
Svelte 5 + Vite + Tailwind + svelte-query 全部保留，页面、组件、i18n、Markdown 净化、分页等
契约都不变。只新增：`/boot.js` 启动信息、桌面实时事件通道、Agent 与 Provider 设置页。

## 2. 目录结构

```
main.go                 入口：无参数 → 桌面 GUI；子命令 → CLI（serve/chat/items/events/log/...）
gui_on.go / gui_off.go  构建标签 !nogui / nogui（nogui 构建无 cgo、无 Wails，用于 CI 与 E2E）
internal/config         环境变量与路径
internal/kernel         领域无关内核：事件日志、游标、邮箱、Runtime、执行、工作项树、工作区、
                        策略文件、记忆文件、调度定义、分诊规则、传播规则、通知 hub
internal/settings       settings.json：providers、角色分配、CLI 路径；预设；密钥脱敏
internal/agentcli       本机 agent CLI 驱动：claude / codex / pi + 探测 + 登录 shell PATH 解析
internal/guard          副作用闸门（capture 阶段）：内置拒绝表 + 分层 POLICY.json + 待读输入暂停；
                        以 `hidane guard` 子命令作为 claude/codex 的 PreToolUse hook 与 pi 扩展的后端
internal/agents         三角色一个循环：think、primary、manager、ingress、workerpool、distiller、
                        livetext、replystream
internal/projections    worklog、archive、board、conversation（只读派生视图）
internal/connectors     heartbeat、triage loop、scheduler、webhook、feishu（长连接模式）
internal/api            http.Handler：/api/*、/api/events/stream（SSE）、/boot.js、/webhook、/health、静态资源
internal/app            组装：打开存储、启动 runtime 与连接器、生命周期
internal/desktop        Wails v3 应用（!nogui）：窗口、单实例、事件桥、GUI 冒烟模式
frontend/               Svelte 5 SPA（由 apps/web 迁入）+ Playwright 测试
cmd/fakeagent           假 claude/codex/pi：按真实协议输出，供 Go 测试与 E2E 使用
```

`apps/server`（Node）在 Go 版本功能对齐并通过全部测试后删除；Docker / Coolify 部署文件随之移除
（桌面客户端不再需要容器部署；headless `hidane serve` 保留给 E2E 与远程使用）。

## 3. 存储：Postgres → SQLite

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

## 4. Agent：本机 claude / codex / pi

### 4.1 统一驱动接口

```go
type Driver interface {
    Kind() Kind                               // claude | codex | pi
    Start(ctx, Request) (Session, error)      // 启动一次运行
}
type Session interface {
    Steer(text string) error                  // 运行中追加人的输入
    Cancel()                                  // 中止（进程组 SIGTERM→SIGKILL）
    Wait() Result                             // 文本、错误、会话 id、工具调用数、策略拦截
}
```

`Request` 携带：prompt、追加系统提示（角色 charter）、cwd、图片、是否允许工具（Primary/Manager/
Distiller 只推理不用工具，Worker 用工具）、模型/effort、已解析的 provider（env 与参数）、可续接的
原生会话 id、闸门环境、文本增量与工具事件回调。

| | claude | codex | pi |
|---|---|---|---|
| 启动 | `claude -p --input-format stream-json --output-format stream-json --verbose --include-partial-messages --replay-user-messages --append-system-prompt <charter>` | `codex exec --json --skip-git-repo-check -C <cwd> -`（prompt 走 stdin，charter 置于 prompt 前） | `pi --mode rpc --no-extensions -e <guard-shim> --append-system-prompt <charter> --session-dir <dir>` |
| 只推理 | `--tools ""` | `--sandbox read-only` | `--no-tools --no-skills --no-context-files --no-prompt-templates` |
| Worker 工具 | `--permission-mode bypassPermissions` + `--settings` 注入 PreToolUse hook → `hidane guard --format claude` | `--sandbox workspace-write` + `-c hooks.PreToolUse=…` + `--dangerously-bypass-hook-trust`（只信任 hidane 自己的 hook） | guard shim 扩展在 `tool_call` 时调用 `hidane guard --format pi` |
| 流式文本 | `stream_event` 的 `text_delta` | `item.completed`（agent_message，整段） | `message_update` 的 `text_delta` |
| 工具事件 | `assistant.tool_use` / `user.tool_result` | `item.started/completed`（command_execution、file_change…） | `tool_execution_start/end` |
| 追加输入 | stdin 写入新的 user 消息（排队到下一轮） | 本轮结束后 `codex exec resume <thread_id>` 继续同一执行 | RPC `steer` |
| 中止 | 杀进程组 | 杀进程组 | RPC `abort` 后杀进程组 |
| 续接（Manager 连续性） | `--resume <session_id>` | `exec resume <thread_id>` | `--session <file>` |

- 所有子进程：独立进程组；Worker cwd 必在工作项工作区内；清理 `CLAUDECODE` 等父会话变量；
  pi 设置 `PI_OFFLINE=1`。
- GUI 应用从 Finder 启动时 PATH 很短：启动时用登录 shell 解析用户 PATH，并补充常见目录
  （`~/.local/bin`、`~/.bun/bin`、`/opt/homebrew/bin`、mise/asdf shims），设置页也可手填绝对路径。
- 探测：`<bin> --version`（超时 10s），结果在设置页与 `/api/status` 展示。

### 4.2 闸门（capture 阶段）统一实现

原 `extensions/pi-guard.ts` 的规则迁入 Go `internal/guard`：内置拒绝表（`sudo`、`rm -rf /`、
`mkfs`、`dd if=`、fork bomb、`shutdown/reboot`、`git push --force`）、`HIDANE_GUARD_DENY` 字面量、
全局→祖先工作区→本工作区的 `POLICY.json`、有待读输入时暂停写操作。三个 CLI 都通过
`hidane guard` 子命令接入同一份逻辑（claude/codex 用 hook 协议，pi 用一个极小的 TS 扩展转调），
规则只读文件、不调模型；被拦截的调用写入 `HIDANE_POLICY_BLOCKS_FILE`，执行结束时作为
`policy.blocked` 事件记入日志。

### 4.3 LLM Provider 配置（参考 paseo）

`HIDANE_HOME/settings.json`（权限 0600，API 响应中密钥只返回是否存在与尾号）：

```json
{
  "providers": [
    { "id": "deepseek", "label": "DeepSeek",
      "anthropicBaseUrl": "https://api.deepseek.com/anthropic",
      "openaiBaseUrl": "", "piProvider": "deepseek",
      "apiKey": "…", "models": ["deepseek-chat"] }
  ],
  "roles": {
    "primary":   { "agent": "claude", "provider": "",         "model": "",              "effort": "low" },
    "manager":   { "agent": "claude", "provider": "",         "model": "",              "effort": "low" },
    "worker":    { "agent": "codex",  "provider": "",         "model": "",              "effort": "medium" },
    "distiller": { "agent": "pi",     "provider": "deepseek", "model": "deepseek-chat", "effort": "low" }
  },
  "binaries": { "claude": "", "codex": "", "pi": "" }
}
```

`provider: ""` 表示使用该 CLI 自己的登录与默认模型（不注入任何东西）。选了 provider 时按 CLI 注入：

- **claude**：需要 `anthropicBaseUrl`（Anthropic 兼容端点）→ `ANTHROPIC_BASE_URL`、`ANTHROPIC_AUTH_TOKEN`、
  `ANTHROPIC_MODEL` / `ANTHROPIC_DEFAULT_{OPUS,SONNET,HAIKU}_MODEL` / `ANTHROPIC_SMALL_FAST_MODEL` 与
  `--model`；第三方端点禁用 `WebSearch`。
- **codex**：需要 `openaiBaseUrl`（Responses API）→ `-c model_provider=hidane`、
  `-c model_providers.hidane.{name,base_url,wire_api="responses",env_key="HIDANE_CODEX_API_KEY"}`，
  进程环境注入 `HIDANE_CODEX_API_KEY`，`-m <model>`。
- **pi**：需要 `piProvider`（pi 内置 provider 名，如 deepseek / moonshot / zai / openrouter / opencode-go）→
  `--provider <p> --model <m> --api-key <key>`。
- effort 映射：claude `--effort`、codex `-c model_reasoning_effort=`、pi `--thinking`。

内置预设：Anthropic、OpenAI、DeepSeek、Moonshot（Kimi）、智谱 GLM（Z.AI）、OpenRouter、OpenCode Go；
预设只是表单预填，保存后就是普通 provider。设置页对「角色 × CLI × provider」的不兼容组合给出明确提示，
后端保存时同样校验（例如 codex 角色选了没有 `openaiBaseUrl` 的 provider → 400）。
「测试」按钮对某个角色做一次真实往返（ping），这是唯一能证明 key/model/端点可用的方式。

设置变更作为事实记入日志（`settings.updated`，不含密钥）。

## 5. 前后端通信

- 一个 `http.Handler`（`internal/api`）：`/api/*`、`/boot.js`、`/health`、`/webhook/:name`、嵌入的 SPA。
- **桌面**：交给 Wails `AssetOptions.Handler`；只有 webview 能访问，因此不需要 token。实时事件由
  Go hub → `app.Event.Emit("hidane:frame", …)`，前端通过 `/wails/runtime.js` 的 `Events.On` 接收。
- **headless `hidane serve`**：同一个 Handler 走普通 HTTP；`/api/*` 需要 `HIDANE_API_TOKEN`（未设置时
  随机生成并打印带 `?token=` 的地址）；实时事件走 SSE（`hidane`/`stream`/`ping`/`hello` 帧不变）。
- `/boot.js` 告诉前端运行形态：`window.hidaneBoot = { desktop, auth, version }`。

## 6. 测试体系（参考 magpie）

| 层 | 工具 | 覆盖 |
|---|---|---|
| Go 单元/集成 | `go test ./...`（临时 HOME + 临时 SQLite，`httptest`） | 内核不变量（append-only、游标、邮箱、跳数预算、Runtime 批处理/优先级/毒消息、执行恢复、取消树）、三个 CLI 驱动对假 CLI 的协议解析/追加输入/取消/provider 注入、闸门规则与 hook 协议、设置校验与脱敏、Primary/Manager effect 应用、调度、投影、API 与鉴权、SSE |
| 前端单元 | vitest + @testing-library/svelte | 原有 18 组 + 新增设置模型、实时通道选择 |
| 前端 UI（假后端） | Playwright + `page.route` | 设置页表单、兼容性提示、token 门 |
| E2E（真后端） | Playwright + `hidane serve`（nogui 构建）+ `cmd/fakeagent` | 发消息 → Primary 建工作项 → Manager 派 Worker → Worker 在工作区写文件（经过真实闸门）→ 回复出现在问题下方；设置 provider/角色；调度、记忆、策略、状态页 |
| 桌面冒烟 | `HIDANE_GUI_SMOKE=1` 启动真实 Wails 应用 | webview 加载前端并调用后端成功后自动退出 0 |
| 真实 CLI 冒烟 | `hidane model --ping --role …` / `hidane chat` | 本机 claude / codex / pi 各跑一次真实往返 |
| 验收 | `acceptance/scenarios.md`（自然语言） | 更新为桌面/serve 形态 |

命令：`make test`（go vet + go test + 前端 check/test）、`make e2e`、`make smoke-gui`、`make app`。

## 7. 执行顺序

1. 计划文档（本文）
2. Go 骨架 + 内核（SQLite）+ 内核测试
3. 设置 / agent CLI 驱动 / 闸门 + 假 CLI + 测试
4. 角色循环（primary / manager / ingress / workerpool / distiller）+ 测试
5. 投影、连接器、API、CLI 子命令 + 测试
6. 前端迁入 `frontend/`：boot、实时通道、设置页、i18n、测试
7. Wails 桌面壳 + GUI 冒烟
8. Playwright E2E；真实 CLI 冒烟
9. 飞书连接器（官方 Go SDK，长连接模式，桌面端无需公网回调）
10. 删除 Node 服务端与部署文件，更新 README / AGENTS.md / CLAUDE.md / 验收场景
11. 循环：全部检查 → 修复 → 再检查，直到全绿
