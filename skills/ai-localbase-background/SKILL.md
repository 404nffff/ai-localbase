---
name: ai-localbase-background
description: Use when starting any conversation in a project that uses ai_localbase as its primary knowledge base and may require background queueing, async polling, or project-local sync state.
---

## 运行位置

以下内容是在说明 skill 自身的安装位置与项目运行状态目录，不是让你在项目目录里创建同名说明文件。

- skill 安装目录：`~/.codex/skills/ai-localbase-background/`
- 默认配置文件：`~/.codex/skills/ai-localbase-background/.env`
- 每个项目的运行状态目录：`<project>/docs/.ai-localbase-background/`
- 若误传 `docs/[任务目录]`、`docs/[任务目录]/onlyAI` 或其子目录，入口脚本会自动回退到项目启动目录，再确认知识库和运行状态目录

## 适用场景

- 需要把文档上传改成后台执行
- 需要先返回 `jobId`，稍后轮询状态
- 需要把 worker、日志、任务队列和结果按项目隔离
- 需要保持 `search / chat` 同步返回，不走后台队列
- 需要把知识库映射、worker 日志、任务队列和结果放在项目本地状态目录

## 当前范围

这是最小后台版本，目前只提供：

- `init`
- `tools`
- `list`
- `upload`
- `append`
- `update`
- `delete`
- `worker-start`
- `worker-status`
- `worker-logs`
- `worker-stop`
- `queue-upload`
- `queue-append`
- `queue-update`
- `queue-delete`
- `search`
- `chat`
- `job-status`
- `job-result`

其中：

- 每次进入项目仍然先执行 `init`，用于确认当前目录对应的 `knowledgeBaseId`
- `init` 必须先调用 `tools/list` 罗列工具能力，再调用 `knowledge_base.list` 检索已有知识库，并按 `name == basename(项目启动目录)` 精确匹配；匹配成功后把真实 `knowledgeBaseId` 写入项目状态目录的 `knowledge.json`，没有匹配时才调用 `knowledge_base.create`
- `knowledge.json` 只能作为本地映射缓存，不能只凭缓存跳过服务端 `knowledge_base.list`
- 所有子命令的目录参数都应传项目启动目录；误传任务目录时会自动归一到项目根目录，避免按任务目录创建知识库
- Bash 入口同时内置同步 `upload / append / update / delete / search / chat`
- `queue-*` 在发现 worker 未运行时会自动拉起后台 worker
- 自动拉起的 worker 在队列清空并空闲一小段时间后会自行退出
- `search / chat` 保持同步执行，不进后台队列
- Bash 入口依赖 Python 3 标准库读写 JSON 状态，避免项目级 `knowledge.json` 被字符串拼接写坏

## 使用流程

1. 每次进入项目先执行 `init`
2. 需要审计能力时执行 `tools`，查看服务端开放的工具、调用方式、参数和响应字段
3. 需要核对知识库时执行 `list`，查看已有知识库名称、真实 ID 与文档数量
4. 需要立刻完成写入时，直接调用同步 `upload / append / update / delete`
5. 需要异步写入时，把任务写入 `queue-*`
6. `queue-*` 会在需要时自动启动后台 worker
7. 通过 `job-status` 轮询任务状态
8. 通过 `job-result` 查看最终返回结果
9. 需要即时检索或问答时，直接调用同步 `search / chat`
10. 一般不用手动停 worker；队列处理完并空闲后会自动退出
11. `worker-start / worker-stop` 只用于调试或批量任务排查
12. 当前环境必须具备 Python 3；缺失时先安装 Python 3 再使用后台版入口

## 工具发现与知识库列表

`tools` 子命令调用 JSON-RPC `tools/list`，用于先罗列当前服务端实际开放的工具能力。返回的每个工具必须关注这些字段：

- `name` / `description`：工具名称与用途
- `invocation`：调用方式，包含 JSON-RPC `tools/call` 和普通 HTTP `/api/mcp/tools/:name/call`
- `parameters`：参数列表，包含 `name`、`type`、`required`、`description`
- `inputSchema`：MCP 兼容 JSON Schema
- `response`：执行成功后 `structuredContent` 中的关键响应字段

`list` 子命令调用普通 HTTP 工具 `knowledge_base.list`，用于检索已有知识库。返回结构在 `structuredContent.items[]` 中，每项包含：

- `id` / `knowledgeBaseId`：真实知识库 ID，例如 `kb-3`
- `name`：知识库名称，初始化时必须用它精确匹配项目启动目录 basename
- `description`：知识库描述
- `createdAt`：创建时间
- `documentCount`：文档数量

初始化和 `queue-*` 入队时只能把匹配出的 `id` / `knowledgeBaseId` 当作后续工具调用的 `knowledgeBaseId`。禁止把目录名或知识库名直接当作 ID。

## 检索返回结构

`knowledge_base.search` 返回的是 MCP tool result 外层对象，不能只读取 `content[].text`。`content` 通常只包含“共检索到 N 条结果”这类摘要，真正可用的命中片段在 `structuredContent.items`。

入参结构：

```json
{
  "query": "搜索关键词",
  "knowledgeBaseId": "",
  "documentId": "",
  "topK": 5
}
```

字段规则：

- `query` 必填
- `knowledgeBaseId` 为空表示跨知识库搜索；项目内默认先执行 `init`，再使用当前项目对应的真实 `knowledgeBaseId`
- `documentId` 不为空时只检索单个文档
- `topK` 是 MCP 层二次截断；不传时后端默认跨知识库最多选 10 条、每个文档默认最多 2 条
- `score` 是检索或重排后的相关性分数，越高越相关

返回结构示例：

```json
{
  "content": [
    {
      "type": "text",
      "text": "共检索到 N 条结果"
    }
  ],
  "structuredContent": {
    "items": [
      {
        "knowledgeBaseId": "kb-1",
        "documentId": "doc-1",
        "documentName": "xxx.md",
        "chunkId": "chunk-xxx",
        "text": "命中的原文片段",
        "score": 0.83,
        "index": 0
      }
    ]
  },
  "isError": false
}
```

处理结果时必须遍历 `structuredContent.items`，读取每条命中的 `text`、`documentName`、`documentId`、`chunkId`、`score` 和 `index`。若 `structuredContent.items` 为空，即使 `content[].text` 存在摘要，也应按“未命中可用片段”处理。

## Bash 示例

```bash
# 每次进入项目先 init，确认知识库 ID
"${HOME}/.codex/skills/ai-localbase-background/ai-localbase-background.sh" init "/path/to/project"

# 初始化前可显式查看工具能力和已有知识库
"${HOME}/.codex/skills/ai-localbase-background/ai-localbase-background.sh" tools
"${HOME}/.codex/skills/ai-localbase-background/ai-localbase-background.sh" list

# 直接同步上传
"${HOME}/.codex/skills/ai-localbase-background/ai-localbase-background.sh" upload "notes.md" "# 内容" "/path/to/project"

# 直接同步追加
"${HOME}/.codex/skills/ai-localbase-background/ai-localbase-background.sh" append "doc-123" "追加内容" "/path/to/project"

# 直接同步覆盖
"${HOME}/.codex/skills/ai-localbase-background/ai-localbase-background.sh" update "doc-123" "# 新内容" "/path/to/project"

# 直接同步删除
"${HOME}/.codex/skills/ai-localbase-background/ai-localbase-background.sh" delete "doc-123" "/path/to/project"

# 写入上传任务；若 worker 未运行会自动拉起
"${HOME}/.codex/skills/ai-localbase-background/ai-localbase-background.sh" queue-upload "notes.md" "# 内容" "/path/to/project"

# 追加文档任务
"${HOME}/.codex/skills/ai-localbase-background/ai-localbase-background.sh" queue-append "doc-123" "追加内容" "/path/to/project"

# 覆盖文档任务
"${HOME}/.codex/skills/ai-localbase-background/ai-localbase-background.sh" queue-update "doc-123" "# 新内容" "/path/to/project"

# 删除文档任务
"${HOME}/.codex/skills/ai-localbase-background/ai-localbase-background.sh" queue-delete "doc-123" "/path/to/project"

# 查询任务状态
"${HOME}/.codex/skills/ai-localbase-background/ai-localbase-background.sh" job-status "job-123" "/path/to/project"

# 查询任务结果
"${HOME}/.codex/skills/ai-localbase-background/ai-localbase-background.sh" job-result "job-123" "/path/to/project"

# 查看 worker 日志
"${HOME}/.codex/skills/ai-localbase-background/ai-localbase-background.sh" worker-logs "/path/to/project" 100

# 同步检索与问答
"${HOME}/.codex/skills/ai-localbase-background/ai-localbase-background.sh" search "关键词" "/path/to/project"
"${HOME}/.codex/skills/ai-localbase-background/ai-localbase-background.sh" search "/path/to/project" "关键词" 5
"${HOME}/.codex/skills/ai-localbase-background/ai-localbase-background.sh" chat "你的问题" "/path/to/project"

# 停止 worker
"${HOME}/.codex/skills/ai-localbase-background/ai-localbase-background.sh" worker-stop "/path/to/project"
```

## PowerShell 示例

```powershell
& "$HOME/.codex/skills/ai-localbase-background/ai-localbase-background.ps1" init "C:\work\project"
& "$HOME/.codex/skills/ai-localbase-background/ai-localbase-background.ps1" tools
& "$HOME/.codex/skills/ai-localbase-background/ai-localbase-background.ps1" list
& "$HOME/.codex/skills/ai-localbase-background/ai-localbase-background.ps1" upload "notes.md" "# 内容" "C:\work\project"
& "$HOME/.codex/skills/ai-localbase-background/ai-localbase-background.ps1" append "doc-123" "追加内容" "C:\work\project"
& "$HOME/.codex/skills/ai-localbase-background/ai-localbase-background.ps1" update "doc-123" "# 新内容" "C:\work\project"
& "$HOME/.codex/skills/ai-localbase-background/ai-localbase-background.ps1" delete "doc-123" "C:\work\project"
& "$HOME/.codex/skills/ai-localbase-background/ai-localbase-background.ps1" queue-upload "notes.md" "# 内容" "C:\work\project"
& "$HOME/.codex/skills/ai-localbase-background/ai-localbase-background.ps1" queue-append "doc-123" "追加内容" "C:\work\project"
& "$HOME/.codex/skills/ai-localbase-background/ai-localbase-background.ps1" queue-update "doc-123" "# 新内容" "C:\work\project"
& "$HOME/.codex/skills/ai-localbase-background/ai-localbase-background.ps1" queue-delete "doc-123" "C:\work\project"
& "$HOME/.codex/skills/ai-localbase-background/ai-localbase-background.ps1" job-status "job-123" "C:\work\project"
& "$HOME/.codex/skills/ai-localbase-background/ai-localbase-background.ps1" job-result "job-123" "C:\work\project"
& "$HOME/.codex/skills/ai-localbase-background/ai-localbase-background.ps1" search "关键词" "C:\work\project"
& "$HOME/.codex/skills/ai-localbase-background/ai-localbase-background.ps1" chat "你的问题" "C:\work\project"
& "$HOME/.codex/skills/ai-localbase-background/ai-localbase-background.ps1" worker-stop "C:\work\project"
```

## 状态目录

后台版会在项目 `docs/` 下维护：

- `docs/.ai-localbase-background/knowledge.json`
- `docs/.ai-localbase-background/worker.pid`
- `docs/.ai-localbase-background/worker.log`
- `docs/.ai-localbase-background/queue/`
- `docs/.ai-localbase-background/jobs/`
- `docs/.ai-localbase-background/results/`

## 注意点

- Bash 入口现在同时支持同步 `upload / append / update / delete / search / chat`
- Bash 与 PowerShell 入口都支持 `tools / list`，用于查看工具能力和已有知识库
- `init` 和 `queue-*` 都会先通过 `knowledge_base.list` 按项目名匹配已有知识库，未命中才创建
- `queue-upload / queue-append / queue-update / queue-delete` 在有 Python 时只负责投递任务，不保证任务立刻完成
- 若 `worker` 没启动，`queue-*` 会自动拉起一个后台 worker
- Bash 入口需要 Python 3；缺失时 `init`、同步命令、`queue-*`、`worker-*` 和 `job-*` 都不可用
- `job-result` 只有在任务成功或失败后才有结果
- `search / chat` 不依赖 worker，继续按同步方式立即返回
- 自动拉起的 worker 会在队列清空并空闲后自动退出
- 运行状态目录建议加入项目 `.gitignore`
