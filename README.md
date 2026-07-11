# Go-Claw

Go-Claw 是一个面向本地工作区的 AI Agent 框架，支持工具调用、长期记忆、可压缩会话上下文、定时任务和实时消息推送。

## ✨ 特性

### 核心功能
- **工具调用** - Agent 可以循环执行工具（命令、文件、待办事项等）
- **双层会话存储** - SQLite 保存业务消息，JSONL 保存包含工具调用的完整模型上下文
- **多平台支持** - Telegram、飞书、Discord 集成
- **长期记忆** - 每轮结束后异步提取事实偏好、程序、情景和语义记忆
- **上下文管理** - 工作区文件、运行环境注入和三层自动压缩
- **多 LLM 提供商** - OpenAI、Anthropic、Ollama

### 高级功能
- **流式输出** - 支持实时流式响应，逐字显示内容，实时展示工具调用状态
- **定时任务系统** - 支持一次性提醒、周期性任务和 Cron 表达式
- **WebSocket 实时推送** - 前端自动接收新消息，无需轮询
- **Dashboard Web 界面** - 可视化管理会话、Agent、技能和定时任务
- **RESTful API** - 完整的 API 用于集成和自动化
- **技能系统** - 可扩展的技能管理，支持自定义技能
- **上下文用量展示** - 每条助手回复显示当前 token 占用和上下文窗口百分比
- **Markdown 消息** - Dashboard 安全渲染 CommonMark/GFM、代码块和表格

## 🚀 快速开始

### 构建 CLI
```bash
go build -o go-claw-cli.exe ./cmd/cli/...
```

### 运行 CLI
```bash
./go-claw-cli.exe
```

### 运行 Dashboard
```bash
go build -o go-claw-server.exe ./cmd/go-claw/...
./go-claw-server.exe
```

然后访问 http://localhost:18789

## 📋 CLI 命令

| 命令 | 描述 |
|------|------|
| `/new` | 开始新的对话 |
| `/sessions` | 列出所有会话 |
| `/switch <id>` | 切换到指定会话 |
| `/delete <id>` | 删除会话 |
| `/clear` | 清除当前对话 |
| `/help` | 显示帮助信息 |

## 📁 项目结构

```
go-claw/
├── cmd/
│   ├── cli/              # CLI 应用程序
│   └── go-claw/          # 服务器应用程序
├── internal/
│   ├── agent/            # Agent 核心（manager.go, agent.go, session.go, context.go）
│   ├── llm/              # LLM 提供商（OpenAI, Anthropic, Ollama）
│   ├── tools/            # 工具实现（exec, file, todo, time 等）
│   ├── storage/          # 数据库模型和仓库
│   ├── server/           # HTTP/WebSocket 服务器
│   ├── dashboard/        # Dashboard Web 界面
│   ├── scheduler/        # 定时任务调度器
│   └── platform/         # 平台集成（Telegram, Feishu）
├── workspace/            # 示例 Agent 工作区（实际位置由 work_dir 决定）
│   ├── IDENTITY.md      # Agent 身份
│   ├── SOUL.md          # 行为原则
│   ├── USER.md          # 用户信息
│   ├── MEMORY.md        # 长期记忆索引
│   ├── AGENTS.md        # Agent 路由规则
│   ├── memory/          # 独立长期记忆文件
│   └── sessions/        # 完整模型上下文 JSONL 账本
└── README.md
```

## 🎯 定时任务系统

### 任务类型
- **一次性提醒** (`at`) - 在指定时间执行一次
- **周期性任务** (`every`) - 每隔固定时间执行
- **Cron 任务** (`cron`) - 使用 Cron 表达式定义执行时间

### 会话目标
- `main` - 使用主会话上下文
- `isolated` - 在专用会话中运行
- `current` - 绑定到当前会话
- `session:<id>` - 绑定到指定会话

### 负载类型
- `systemEvent` - 主会话事件
- `agentTurn` - 隔离的 Agent 回合

### API 示例

```bash
# 创建定时任务
POST /api/scheduled-tasks
{
  "name": "每日新闻",
  "kind": "cron",
  "cron_expression": "0 8 * * *",
  "input": "获取今日新闻摘要",
  "session_target": "main",
  "payload_kind": "systemEvent"
}

# 获取任务列表
GET /api/scheduled-tasks

# 删除任务
DELETE /api/scheduled-tasks?id=1
```

## 🔌 WebSocket 推送

Dashboard 使用 WebSocket 实现实时消息推送：

1. 前端建立 WebSocket 连接到 `/ws`
2. 发送认证消息包含 `session_id`
3. 后端验证并将客户端加入对应会话
4. 新消息到达时自动推送到前端

### 消息格式

```json
{
  "type": "new_message",
  "payload": {
    "session_id": "session_1",
    "message": {
      "content": "回复内容",
      "tool_calls": [
        {
          "tool_name": "exec",
          "input": "{\"program\":\"go\",\"args\":[\"test\",\"./...\"]}",
          "output": "文件列表",
          "success": true
        }
      ]
    }
  }
}
```

## 🌊 流式输出

Go-Claw 支持实时流式输出，让用户能够看到 Agent 的思考和执行过程。

### 工作原理

1. **流式请求** - 前端通过 WebSocket 发送带 `stream: true` 的消息
2. **实时推送** - 后端逐字推送内容，实时显示工具调用状态
3. **状态展示** - 工具执行时显示加载动画，完成后显示结果

### 流式事件类型

| 事件类型 | 描述 |
|---------|------|
| `start` | 开始流式输出 |
| `content` | 内容增量更新 |
| `reasoning` | 思考过程（支持的模型） |
| `tool_call` | 工具调用开始 |
| `tool_result` | 工具调用结果 |
| `iteration` | 迭代进度 |
| `complete` | 输出完成 |
| `error` | 错误信息 |

### 流式消息示例

```json
// 开始事件
{"type": "stream_event", "payload": {"type": "start", "timestamp": 1234567890}}

// 内容增量
{"type": "stream_event", "payload": {"type": "content", "data": {"content": "你好", "delta": true}}}

// 工具调用
{"type": "stream_event", "payload": {"type": "tool_call", "data": {"tool_name": "web_search", "input": "天气"}}}

// 工具结果
{"type": "stream_event", "payload": {"type": "tool_result", "data": {"tool_name": "web_search", "output": "...", "success": true}}}

// 完成事件
{
  "type": "stream_event",
  "payload": {
    "type": "complete",
    "data": {
      "content": "完整内容",
      "message_id": "msg_xxx",
      "input_tokens": 4200,
      "output_tokens": 380,
      "context_usage": {
        "used_tokens": 4580,
        "window_tokens": 200000,
        "percent": 2.29,
        "estimated": false
      }
    }
  }
}
```

### 前端切换

Dashboard 支持流式/普通模式切换：
- **流式模式**（默认）：消息逐字显示，实时展示工具执行
- **普通模式**：等待完整响应后一次性显示

切换按钮位于输入框旁边的「流式」开关。

## 🛠️ 工具系统

### 内置工具
- **exec** - 执行命令或程序
- **read_file** - 读取文件内容
- **write_file** - 写入文件
- **list_dir** - 列出目录文件
- **todo_write** - 管理待办事项
- **create_scheduled_task** - 创建定时任务
- **create_reminder** - 创建一次性提醒
- **web_search** - 网页搜索（使用 DuckDuckGo）
- **web_fetch** - 获取网页内容

### 工具调用流程
1. Agent 分析用户请求
2. 决定调用哪些工具
3. 并行执行工具
4. 收集结果并生成回复
5. 将 user/assistant 业务消息保存到数据库，并将完整工具上下文写入 Session JSONL

## 🎯 技能系统

技能系统允许你为 Agent 添加专业能力，每个技能都是一个独立的模块。

### 技能格式

技能使用 YAML frontmatter 格式定义：

```yaml
---
name: 天气查询
command: /天气
description: 查询指定城市的天气信息
version: 1.0.0
author: user
category: utility
tags:
  - weather
  - utility
tools:
  - web_search
  - web_fetch
variables:
  default_city: 北京
---

## 使用说明

当用户询问天气时，使用 web_search 工具搜索天气信息。

## 示例

用户：今天北京天气怎么样？
助手：让我帮你查询北京的天气...
```

### 技能管理

- **Dashboard 界面** - 在「技能」页面可视化管理
- **API 接口** - 通过 REST API 创建、更新、删除技能
- **文件系统** - 直接编辑 `skills/` 目录下的 SKILL.md 文件

## 🌐 Dashboard 界面

访问 http://localhost:18789 打开 Dashboard：

- **会话列表** - 查看所有对话历史
- **Agent 管理** - 配置和管理 Agent
- **技能管理** - 创建、编辑、删除技能
- **定时任务** - 创建、编辑、删除定时任务
- **任务日志** - 查看任务执行历史
- **设置** - 系统配置
- **Markdown 消息** - 助手回复支持标题、列表、引用、代码块、表格和链接
- **上下文占用** - 助手消息下方显示已用 token、窗口大小和占用百分比

### 界面预览

<img src="./resources/web.jpeg" alt="Dashboard 界面" width="800"/>

### Markdown 与安全

助手消息使用服务端 Goldmark 渲染 CommonMark/GFM。历史消息和普通响应直接返回渲染后的 `content_html`；流式响应在生成过程中显示纯文本，在 `complete` 后通过 `/api/markdown` 渲染一次。原始 HTML 默认禁用，避免模型输出中的 `<script>`、事件属性等内容进入页面。

用户消息始终作为纯文本显示。工具调用区域与 Markdown 正文使用独立 DOM 容器，重新渲染正文不会覆盖工具输入、输出或上下文占用信息。

## 📝 工作区文件

Go-Claw 使用特殊文件来维护上下文，默认位于 `~/.go-claw` 目录：

- **IDENTITY.md** - Agent 身份；为空时默认使用 `go-claw agent`
- **SOUL.md** - Agent 行为原则；为空时使用内置可靠性与安全原则
- **USER.md** - 用户信息、偏好和约束
- **MEMORY.md** - 可直接召回的长期记忆核心内容和详细文件链接
- **AGENTS.md** - Agent 路由或协作规则
- **BOOT.md** - 启动指令
- **HEARTBEAT.md** - 日常检查项
- **SKILLS.md** / `skills/` - 可用技能定义

系统提示词还会动态注入当前时间、时区、操作系统、架构、主机名、系统用户、工作目录和对应平台的 Shell 使用提示。只有标题而没有正文的工作区文件不会产生空章节。

### 工作区位置

- **默认位置**: `~/.go-claw` (用户目录下的 .go-claw 文件夹)
- **配置文件**: `~/.go-claw/config.yaml`
- **数据库**: `~/.go-claw/data/go-claw.db`

可以通过配置文件中的 `work_dir` 和 `database.path` 选项自定义位置：

```yaml
work_dir: /path/to/your/workspace
database:
  path: /path/to/your/database.db
```

## 🧠 长期记忆

启用 `memory.enabled` 后，每轮主 Agent 响应完成都会异步创建临时记忆 Agent。记忆 Agent 仅能调用专用的 `record_memory` 工具；没有值得保存的信息时不会调用工具。

支持四类记忆：

| 类型 | 内容 |
|------|------|
| `fact_preference` | 用户事实、稳定偏好、约束和习惯 |
| `procedural` | 可复用的操作步骤、工作流和解决方法 |
| `episodic` | 值得在未来回忆的事件、决定和结果 |
| `semantic` | 项目知识、概念、实体关系和结论 |

每条记忆保存在 `<work_dir>/memory/<简要概述>.md`：

```markdown
---
timestamp: 2026-07-11T18:11:28+08:00
summary: "用户姓名"
type: fact_preference
---

用户姓名：xxx
```

根目录 `MEMORY.md` 是混合索引：核心事实直接进入系统上下文，复杂细节保留链接，只有核心内容不足时才需要读取独立记忆文件。服务启动时会根据已有记忆文件重建索引。

## 🗜️ 会话上下文与三层压缩

Go-Claw 使用两套互补存储：

- **数据库**：保存 user/assistant 业务消息，用于 Dashboard、会话列表和审计。
- **`sessions/<数据库会话ID>.jsonl`**：保存模型完整历史，包括工具调用、工具结果和摘要边界。

每轮调用模型前，从最近一条 summary 边界开始读取有效 JSONL 历史，并创建不会修改原始账本的内存压缩视图：

1. **微压缩**：最近 10 轮之前、超过 500 字节且可重新读取的 tool result，被截断为 500 字节并添加压缩标记。
2. **半窗口清理**：占用率超过 50% 时，在内存视图中清空这些旧 tool result，并标记为可重新调用工具获取。
3. **全量摘要**：占用率超过 90% 时，Summary Agent 总结当前有效历史，并向 JSONL 追加一条 `<conversation_summary>` 历史消息。

JSONL 中摘要之前的原始记录不会删除。向模型组装历史时从文件末尾向前读取，遇到最近一条 summary 后停止，因此后续上下文以该摘要为起点。

上下文占用优先使用 Provider 返回的 token 数；Provider 不提供 usage 时使用保守估算，并在 API 中设置 `estimated: true`。

## 🔧 配置

### 首次运行

第一次运行时，如果找不到配置文件，程序会自动在 `~/.go-claw/config.yaml` 创建默认配置文件。

### 配置文件位置

配置文件会按以下顺序查找（找到第一个就使用）：
1. `./config.yaml` - 当前目录
2. `./configs/config.yaml` - configs 目录  
3. `~/.go-claw/configs/config.yaml` - 用户目录（默认创建位置）

### 配置文件示例

```yaml
server:
  host: "127.0.0.1"
  port: 18789
  
database:
  type: "sqlite"
  path: "~/.go-claw/data/go-claw.db"

llm_provider:
  provider: "ark"
  model: "minimax-m2.5"
  base_url: "https://ark.cn-beijing.volces.com/api/coding/v3"
  api_key: "YOUR_API_KEY"  # 请替换为你的 API 密钥
  max_tokens: 200000
  timeout: 120s
  temperature: 0.2

skills:
  directory: "./skills"
  max_injected_skills: 3
  max_injection_chars: 4000

# 长期记忆：每轮结束后由临时记忆 Agent 异步提取并写入工作区
memory:
  enabled: true
  model: ""             # 留空则使用主 Agent 模型
  timeout: 120s
  max_input_chars: 50000

# 模型上下文账本与三层压缩
context:
  enabled: true
  window_tokens: 200000
  recent_turns: 10
  tool_result_max_bytes: 500
  compact_threshold_percent: 50
  summary_threshold_percent: 90

# 自定义工作区目录（可选）
# work_dir: "./workspace"

log:
  level: "info"
  format: "console"
```

### 重要配置项

**必须配置：**
- `llm_provider.api_key` - 你的 LLM API 密钥

**可选配置：**
- `work_dir` - 工作区目录，默认 `~/.go-claw`
- `memory.enabled` - 是否启用异步长期记忆，默认 `true`
- `memory.model` - 记忆提取模型，留空时使用主 Agent 模型
- `context.window_tokens` - 模型上下文窗口，用于计算占用比例，默认 `200000`
- `context.recent_turns` - 不参与旧工具结果压缩的最近轮数，默认 `10`
- `context.tool_result_max_bytes` - 第一层工具结果保留字节数，默认 `500`
- `context.compact_threshold_percent` - 第二层清理阈值，默认 `50`
- `context.summary_threshold_percent` - Summary Agent 触发阈值，默认 `90`
- `database.path` - 数据库路径，默认 `~/.go-claw/data/go-claw.db`
- `server.port` - HTTP 服务端口，默认 `18789`

## 📚 开发指南

### 添加新工具

1. 在 `internal/tools` 目录创建新文件
2. 实现工具函数
3. 在工具注册表中注册

### 添加新平台

1. 在 `internal/platform` 目录创建新文件
2. 实现平台接口
3. 在配置中添加平台设置

### 自定义 Agent 行为

1. 编辑 `workspace/SOUL.md` 修改个性
2. 编辑 `workspace/USER.md` 添加用户偏好
3. 通过 Dashboard 调整 Agent 配置

## 🐛 故障排除

### 常见问题

**Q: WebSocket 连接失败**
- 检查服务器是否启动
- 确认端口配置正确
- 查看浏览器控制台错误信息

**Q: 定时任务不执行**
- 检查 Cron 表达式格式（5 个字段：分 时 日 月 周）
- 确认任务状态为 active
- 查看任务日志了解执行详情

**Q: 工具调用失败**
- 检查工具参数是否正确
- 查看 Agent 日志了解错误详情
- 确认有执行权限

## 📄 许可证

MIT License

## 🙏 致谢

感谢所有贡献者和开源项目！
