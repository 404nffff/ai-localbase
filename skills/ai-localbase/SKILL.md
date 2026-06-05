---
name: ai-localbase
description: Use when starting any conversation in a project that uses ai_localbase as its primary knowledge base - establishes per-session ai-localbase initialization, directory-scoped knowledge mapping, and default knowledge retrieval before responding
---

## 运行位置

以下内容是在说明 skill 的运行位置，不是让你在项目目录里创建一个名为“固定运行目录”或“运行位置”的文件。

- 项目内维护目录：`skills/ai-localbase/`
- 实际运行目录：`${HOME}/.codex/skills/ai-localbase/`
- 配置文件：`${HOME}/.codex/skills/ai-localbase/.env`
- 知识库映射缓存：`${HOME}/.codex/skills/ai-localbase/knowledge.json`
- Bash 入口：`${HOME}/.codex/skills/ai-localbase/ai-localbase.sh`
- PowerShell 入口：`${HOME}/.codex/skills/ai-localbase/ai-localbase.ps1`

# AI LocalBase 项目知识库入口

本 skill 的设计口径是：在启用了 `ai_localbase` 的项目里，每次会话启动时默认加载，并优先完成当前启动目录对应知识库的初始化。

本 skill 提供两个统一入口：

- `ai-localbase.sh`：Bash 版本
- `ai-localbase.ps1`：Windows / PowerShell 版本

两者都使用同一套子命令：`init`、`tools`、`list`、`upload`、`append`、`update`、`delete`、`search`、`chat`，并按目录自动复用 `knowledgeBaseId`。其中 `init` 会把你传入的启动目录 basename 映射为知识库名，例如 `/mnt/sync2/www/agents -> agents`。若误传 `docs/[任务目录]` 或其子目录，入口脚本会自动回退到项目启动目录后再确认知识库，避免按任务目录创建知识库。

## 使用场景

**适用**：
- 项目把 `ai_localbase` 作为主知识库
- 需要在会话启动时初始化当前目录对应的知识库
- 需要批量操作知识库、文档、检索或问答
- 需要排查目录映射、缓存或脚本行为问题

**不适用**：
- 项目未启用 `ai_localbase`
- 上传二进制文件（仅支持文本）
- 批量上传目录（需客户端遍历后逐文件上传）

## 核心规则

1. **会话初始化**：每次会话开始时先进入 `${HOME}/.codex/skills/ai-localbase/` 目录并执行 `init` 子命令，让脚本自动加载同目录下的 `.env`，再把启动目录映射到其 basename 对应的知识库名，例如 `/mnt/sync2/www/agents -> agents`
2. **认证安全**：Token 存入环境变量，避免命令历史泄露
3. **目录隔离**：每个启动目录映射到其 basename 对应的独立知识库，映射缓存写入 `knowledge.json`
4. **任务目录归一**：所有子命令的目录参数都应传当前项目启动目录；若传入 `docs/[任务目录]`、`docs/[任务目录]/onlyAI` 或其子目录，脚本会自动归一到项目根目录后再计算知识库名
5. **初始化握手顺序**：`init` 必须先调用 `tools/list` 罗列工具能力，再调用 `knowledge_base.list` 检索已有知识库，并按 `name == basename(项目启动目录)` 精确匹配；匹配成功后把真实 `knowledgeBaseId` 写入 `knowledge.json`，没有匹配时才调用 `knowledge_base.create`
6. **禁止缓存短路**：`knowledge.json` 只能作为本地映射缓存，不能只凭缓存跳过服务端 `knowledge_base.list`；每次初始化都必须重新从服务端知识库列表确认当前项目名对应的真实 ID
7. **检索 vs 问答**：`search` 用于片段检索，`chat` 用于基于知识库上下文的直接问答
8. **文档维护策略**：新文档优先上传；增量内容用 `append`；全文覆盖用 `update`；废弃文档用 `delete`
9. **依赖前置**：Bash 版本依赖 `bash`、`curl`、`python3`；PowerShell 版本依赖 `Invoke-RestMethod`

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

初始化时只能把匹配出的 `id` / `knowledgeBaseId` 当作后续 `search`、`chat`、`upload`、`append`、`update`、`delete` 的 `knowledgeBaseId`。禁止把目录名或知识库名直接当作 ID。

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
- `knowledgeBaseId` 为空表示跨知识库搜索；日常项目检索应优先使用 `init` 确认出的当前项目知识库 ID
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

## 快速开始

**1. 配置环境**
```bash
cd "${HOME}/.codex/skills/ai-localbase"
cp .env.example .env
# 编辑 .env 填入实际配置
```

**2. Bash 使用方式**
```bash
# 使用实际运行目录下的 skill 脚本

# 初始化前可显式查看工具能力和已有知识库
"${HOME}/.codex/skills/ai-localbase/ai-localbase.sh" tools
"${HOME}/.codex/skills/ai-localbase/ai-localbase.sh" list

# 初始化目录到知识库的映射，`/www/agents` 会映射为知识库名 `agents`
"${HOME}/.codex/skills/ai-localbase/ai-localbase.sh" init "/path/to/project"

# 上传文档（参数：文件名、内容、目录）
"${HOME}/.codex/skills/ai-localbase/ai-localbase.sh" upload "my-doc.md" "# 内容" "/path/to/project"

# 追加文档内容（参数：documentId、内容、目录）
"${HOME}/.codex/skills/ai-localbase/ai-localbase.sh" append "doc-123" "追加内容" "/path/to/project"

# 覆盖文档内容（参数：documentId、内容、目录）
"${HOME}/.codex/skills/ai-localbase/ai-localbase.sh" update "doc-123" "# 全量新内容" "/path/to/project"

# 删除文档（参数：documentId、目录）
"${HOME}/.codex/skills/ai-localbase/ai-localbase.sh" delete "doc-123" "/path/to/project"

# 检索内容（参数：关键词、目录）
"${HOME}/.codex/skills/ai-localbase/ai-localbase.sh" search "关键词" "/path/to/project"

# 检索内容并限制返回数量（参数：关键词、目录、topK）
"${HOME}/.codex/skills/ai-localbase/ai-localbase.sh" search "关键词" "/path/to/project" 5

# 问答（参数：问题、目录）
"${HOME}/.codex/skills/ai-localbase/ai-localbase.sh" chat "你的问题" "/path/to/project"
```

**3. PowerShell 使用方式**
```powershell
# 初始化目录到知识库的映射，`C:\work\agents` 会映射为知识库名 `agents`
& "$HOME/.codex/skills/ai-localbase/ai-localbase.ps1" tools
& "$HOME/.codex/skills/ai-localbase/ai-localbase.ps1" list
& "$HOME/.codex/skills/ai-localbase/ai-localbase.ps1" init "C:\work\project"

# 上传文档
& "$HOME/.codex/skills/ai-localbase/ai-localbase.ps1" upload "my-doc.md" "# 内容" "C:\work\project"

# 追加文档内容
& "$HOME/.codex/skills/ai-localbase/ai-localbase.ps1" append "doc-123" "追加内容" "C:\work\project"

# 覆盖文档内容
& "$HOME/.codex/skills/ai-localbase/ai-localbase.ps1" update "doc-123" "# 全量新内容" "C:\work\project"

# 删除文档
& "$HOME/.codex/skills/ai-localbase/ai-localbase.ps1" delete "doc-123" "C:\work\project"

# 检索
& "$HOME/.codex/skills/ai-localbase/ai-localbase.ps1" search "关键词" "C:\work\project"

# 问答
& "$HOME/.codex/skills/ai-localbase/ai-localbase.ps1" chat "你的问题" "C:\work\project"
```

统一入口会自动：
- 加载 `.env` 配置
- 将 `docs/[任务目录]` 类入参归一到项目启动目录
- 先调用 `tools/list` 罗列工具能力，再调用 `knowledge_base.list` 检索已有知识库
- 按项目启动目录 basename 精确匹配知识库名，匹配成功则刷新 `knowledge.json`
- 未匹配到已有知识库时才调用 `knowledge_base.create`
- 执行对应操作

## 使用流程

1. **先进入实际运行目录**：所有命令、`.env`、`knowledge.json` 都在 `${HOME}/.codex/skills/ai-localbase/` 目录下
2. **需要审计能力时跑 `tools`**：确认服务端开放了哪些工具、每个工具如何调用、哪些参数必填或选填、响应字段是什么
3. **需要核对知识库时跑 `list`**：查看现有知识库名称、真实 ID 与文档数量
4. **会话开始先跑 `init`**：传入当前项目启动目录，让脚本按 `tools/list -> knowledge_base.list -> name 精确匹配 -> 必要时 create` 的顺序确认知识库并刷新本地映射
5. **先用 `search` 查历史**：需要看片段、找已有方案、确认历史决策时优先使用
6. **需要直接结论时用 `chat`**：让知识库基于现有文档输出精简答案
7. **新增内容用 `upload`**：把新的任务文档、摘要或阶段结论写入当前目录对应的知识库
8. **已有文档按需维护**：过程记录优先 `append`；需要整篇替换时用 `update`；废弃文档用 `delete`
9. **收尾时再统一整理**：过程里先保持增量沉淀，阶段收尾再做集中整理和归档

## 常见问题

| 问题 | 原因与解决 |
|------|-----------|
| `401`/`403` 错误 | 检查 `.env` 中的 `MCP_AUTH_TOKEN` 是否正确，以及目标服务是否可访问 |
| `knowledgeBaseId` 为空 | 先执行 `list` 确认是否存在 `name == 当前项目目录名` 的知识库，再重新执行 `init` |
| 不知道 `documentId` | `upload` 的返回结果里会带文档 ID；`search` 命中结果里也会带 `documentId` |
| 无法上传目录 | 需客户端先遍历目录，再逐文件调用 `upload` |
| 搜索返回空 | 确认文档已上传并完成索引，尝试缩小 `query` 或增大 `topK` |
| 文本里有引号或换行 | Bash / PowerShell 入口都已处理转义，直接通过脚本传参即可 |
