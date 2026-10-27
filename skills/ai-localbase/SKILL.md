---
name: ai-localbase
description: Use when starting any conversation in a project that uses ai_localbase as its primary knowledge base - establishes per-session ai-localbase initialization, directory-scoped knowledge mapping, and default knowledge retrieval before responding
---

## 运行位置

以下内容是在说明 skill 的运行位置，不是让你在项目目录里创建一个名为“固定运行目录”或“运行位置”的文件。

- 项目内维护目录：`skills/ai-localbase/`
- 实际运行目录：`${HOME}/.agents/skills/ai-localbase/`
- 配置文件：`${HOME}/.agents/skills/ai-localbase/.env`
- 知识库映射缓存：`${HOME}/.agents/skills/ai-localbase/knowledge.json`
- Linux / Bash 入口：`${HOME}/.agents/skills/ai-localbase/ai-localbase.sh`
- macOS 入口：`${HOME}/.agents/skills/ai-localbase/ai-localbase-macos.sh`
- PowerShell 入口：`${HOME}/.agents/skills/ai-localbase/ai-localbase.ps1`

# AI LocalBase 项目知识库入口

本 skill 的设计口径是：在启用了 `ai_localbase` 的项目里，每次会话启动时默认加载，并优先完成当前启动目录对应知识库的初始化。

按运行平台选择入口：

- `ai-localbase.sh`：Linux / Bash 版本
- `ai-localbase-macos.sh`：macOS 版本，使用系统 `/bin/bash`，兼容 Bash 3.2
- `ai-localbase.ps1`：Windows / PowerShell 版本

各入口都使用同一套核心子命令：`init`、`tools`、`call`、`list`、`upload`、`append`、`update`、`delete`、`search`、`chat`，并按目录自动复用项目知识库绑定。`call` 可按工具名和 arguments JSON 调用 `tools/list` 发现的任意 MCP 工具；PowerShell 入口额外提供 `upload-file`、`append-file`、`update-file`，用于从文件读取正文，避免多行 Markdown 中的反引号、引号和换行被 PowerShell 重新解析。`init` 会把你传入的启动目录 basename 映射为知识库名，例如 `/mnt/sync2/www/agents -> agents`。若误传 `docs/[任务目录]` 或其子目录，入口脚本会自动回退到项目启动目录后再确认知识库，避免按任务目录创建知识库。

macOS 优先使用 `bash ai-localbase-macos.sh`，不要用 `sh` 或 `zsh` 解释该 Bash 脚本。它引用同目录 `ai-localbase.sh` 的命令实现，仅替换配置加载方式；两份脚本须一起保留，共用原有 `.env` 和 `knowledge.json`，无需复制凭据或安装新版 Bash。下文通用 Bash 示例在 macOS 上将入口替换为 `bash ai-localbase-macos.sh`，参数不变；完整路径示例见“macOS 使用方式”。

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

1. **会话初始化**：每次会话开始时先进入 `${HOME}/.agents/skills/ai-localbase/` 目录并执行 `init` 子命令，让脚本自动加载同目录下的 `.env`，再把启动目录映射到其 basename 对应的知识库名，例如 `/mnt/sync2/www/agents -> agents`
2. **认证安全**：Token 存入环境变量，避免命令历史泄露
3. **目录隔离**：每个启动目录映射到其 basename 对应的项目知识库绑定，映射缓存写入 `knowledge.json`
4. **任务目录归一**：所有子命令的目录参数都应传当前项目启动目录；若传入 `docs/[任务目录]`、`docs/[任务目录]/onlyAI` 或其子目录，脚本会自动归一到项目根目录后再计算知识库名
5. **初始化握手顺序**：`init` 必须先调用 `tools/list` 罗列工具能力，再调用 `knowledge_base.list` 检索已有知识库；按精确名称、名称前缀和描述路径匹配项目绑定库；匹配成功后把主 `knowledgeBaseId` 继续写入 `knowledge.json` 顶层旧格式字段，并把多库绑定写入 `_bindings`，没有匹配时才调用 `knowledge_base.create`
6. **禁止缓存短路**：`knowledge.json` 只能作为本地映射缓存，不能只凭缓存跳过服务端 `knowledge_base.list`；每次初始化都必须重新从服务端知识库列表确认当前项目名对应的真实 ID
7. **检索 vs 问答**：`search` 用于片段检索，`chat` 用于基于知识库上下文的直接问答
8. **文档维护策略**：新文档优先上传；增量内容用 `append`；全文覆盖用 `update`；废弃文档用 `delete`
9. **PowerShell 正文传参安全**：多行 Markdown、包含反引号或引号的内容，必须优先写入临时文件或正文文件，再用 `upload-file` / `append-file` / `update-file`；原 `upload` / `append` / `update` 的内容参数也支持 `@文件路径` 或 `file:文件路径`
10. **依赖前置**：Bash 版本依赖 `bash`、`curl`、`python3`；PowerShell 版本依赖 `Invoke-RestMethod`

## 上传前的内容筛选

本节适用于自动沉淀、同步、追加、更新以及通用 `call` 发起的文档写入。由当前会话先判断内容价值，再选择命令；不额外调用第三方 AI，不把筛选任务委派给后台 worker。现有 CLI 不会自动扫描目录、理解正文或维护内容去重索引，不能把这些能力说成已经实现。

### 哪些内容值得入库

先判断“是否属于当前项目、以后能否复用、依据和状态是否清楚”，不要仅凭扩展名或文件所在目录决定。

| 内容 | 自动同步时的处理 |
| --- | --- |
| 业务规则、接口契约、架构约束、决策及取舍原因 | 上传完整有效内容，保留适用范围和来源 |
| 正式计划、设计、施工、状态和测试文档 | 按项目约定同步，标明草案、已确认、已实施或待验证 |
| 故障原因、修复方法、验证结论、重要遗留问题 | 上传可复用的结论及必要证据 |
| 排查过程、会议或操作记录、部署流水 | 提炼结论、关键决策和结果再传；项目明确要求保留的原始文档按约定同步 |
| 寒暄、重复问答、未形成结论的临时尝试、仅进度百分比变化 | 跳过；有未决业务问题时保留问题与待验证状态 |
| 原始终端输出、接口回包、轮询日志、机器状态、缓存、构建产物 | 不自动上传；必要证据以最小片段并入结论 |
| 凭据、令牌、私钥、含秘密的配置或明确禁止读取的文件 | 先按路径和已有信息排除，不为筛选打开文件，不入队 |

默认排除 `docs/.tmp/`、`docs/.ai-localbase-background/`、`node_modules/`、`.git/` 和构建输出目录。不能一刀切排除 `onlyAI/`，其中有业务意义的排查、测试结论仍可同步；不能把所有 `docs/**/*.md` 批量上传。用户明确指定的普通资料仍按其要求处理，不以“价值较低”为由擅自删减；项目规定必须同步的文档也不能静默改成只传摘要。

纯咨询、计划讨论和只读检查不触发写入或建库。只读时先用 `list` 确认当前项目主库；不存在则使用本地资料。仅在确需获准写入时调用可能建库的 `init`。这一约束优先于下文通用的会话初始化示例。

### 选择上传、更新、追加或跳过

- 首次有价值的来源：`upload`。记录项目根、主库真实 ID、来源相对路径（无文件时用稳定主题名）、返回的 documentId，以及实际发送正文的 SHA-256。
- 同一来源已有文档：先完整回读，确认归属和内容；最终正文相同则跳过，有修订则 `update`，不要靠改文件名、追加时间戳重复上传。
- 持续记录有独立的新事实：`append`，只追加尚未记录的增量；更正既有事实用 `update`，或明确写出被替代内容和新结论，避免新旧事实并列。
- 只有相似标题或搜索片段：不能直接认定同一文档。完整回读后确认；无法确认归属时保留为待处理，不猜 documentId、不覆盖别人内容。
- 旧内容失效：优先标记已替代及适用时间；`delete` 需要明确删除授权，不因同步任务自动删除远端文档。

沿用项目已有同步回执记录来源映射和正文哈希，不把 `knowledge.json` 的库映射当作文档去重索引；无回执时先查主库文档并回读，不能把“缓存没有记录”当成“从未上传”。哈希只用于比较内容版本，不证明知识价值，也不能替代更新前的远端回读。远端有其他修改时先核对和合并，不直接覆盖。

批量候选在调用命令前列出“来源、操作、理由、目标 documentId（已有时）”，写入已有任务记录即可，不额外把筛选清单作为知识上传。既有范围授权充分时直接执行，不逐文件反复确认；候选不确定则报告具体缺口，继续处理已明确项。

### 成功和 Wiki 整理分别验收

检查 HTTP、`isError` 与 `structuredContent`，拿到真实文档 ID 后回读内容，确认完整且与预期一致，再更新同步回执。命令退出 0、入队成功或收到 JSON 都不能单独证明入库成功；回读截断时补全读取后才能宣称全文一致。

上传完成只代表原文入库。服务端是否自动整理、使用什么模型及超时，由知识库开关和项目设置决定；需要确认 Wiki 完成时另查 Wiki 任务状态，不能把上传任务成功当成 Wiki 成功。超时或断线后先查目标文档及任务结果，确认是否已经写入，尤其不能盲目重放 `append`。

例如：发布记录应保留版本、目标环境、实际结果和遗留项；完整构建日志与每次健康检查回包留本地，不反复作为新资料上传。

## 工具发现与知识库列表

`tools` 子命令调用 JSON-RPC `tools/list`，用于先罗列当前服务端实际开放的工具能力。返回的每个工具必须关注这些字段：

- `name` / `description`：工具名称与用途
- `invocation`：调用方式，包含 JSON-RPC `tools/call` 和普通 HTTP `/api/mcp/tools/:name/call`
- `parameters`：参数列表，包含 `name`、`type`、`required`、`description`
- `inputSchema`：MCP 兼容 JSON Schema
- `response`：执行成功后 `structuredContent` 中的关键响应字段

`call` 子命令用于调用 `tools` 发现的任意工具，不需要为每个新工具继续修改 Skill 脚本。第二个参数必须是 JSON 对象，对应 MCP `arguments`：

```bash
./ai-localbase.sh call document.list '{"knowledgeBaseId":"kb-1"}'
./ai-localbase.sh call document.detail '{"knowledgeBaseId":"kb-1","documentId":"doc-1"}'
./ai-localbase.sh call document.summarize '{"knowledgeBaseId":"kb-1","documentId":"doc-1"}'
./ai-localbase.sh call structured_data.query '{"documentId":"doc-1","query":"这个表格有多少条记录"}'
./ai-localbase.sh call knowledge_base.health '{"knowledgeBaseId":"kb-1"}'
./ai-localbase.sh call conversation.list '{}'
./ai-localbase.sh call conversation.get '{"conversationId":"conv-1"}'
```

PowerShell 使用相同参数结构：

```powershell
& "$HOME/.agents/skills/ai-localbase/ai-localbase.ps1" call "document.detail" '{"knowledgeBaseId":"kb-1","documentId":"doc-1"}'
```

MCP 客户端通常在连接时缓存工具清单；服务端新增工具后，若第一方 `mcp__ai_localbase__*` 工具尚未出现，重新连接 MCP 或开启新会话。`call` 子命令直接调用服务端，不依赖客户端工具命名空间刷新。

`list` 子命令调用普通 HTTP 工具 `knowledge_base.list`，用于检索已有知识库。返回结构在 `structuredContent.items[]` 中，每项包含：

- `id` / `knowledgeBaseId`：真实知识库 ID，例如 `kb-3`
- `name`：知识库名称，初始化时必须用它精确匹配项目启动目录 basename
- `description`：知识库描述
- `createdAt`：创建时间
- `documentCount`：文档数量

初始化时只能把匹配出的 `id` / `knowledgeBaseId` 当作真实知识库 ID。`init` 输出中的 `knowledgeBaseId` 是主库 ID，用于 `upload`、`append`、`update`、`delete`；`knowledgeBaseIds` 是绑定库 ID 集合，用于 `search` 和 `chat` 跨库读取。禁止把目录名或知识库名直接当作 ID。

`knowledge.json` 顶层保持旧格式兼容：

```json
{
  "agents": "kb-381",
  "zhiliao_api": "kb-275",
  "_bindings": {
    "agents": {
      "primaryId": "kb-381",
      "knowledgeBaseIds": ["kb-381", "kb-707"],
      "boundKnowledgeBases": []
    }
  }
}
```

旧脚本继续读取 `agents` 字符串；新脚本读取 `_bindings.agents` 获取多 ID 绑定。

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
- `knowledgeBaseId` 为空表示跨知识库搜索；日常项目检索应优先使用 `init` 确认出的当前项目绑定 ID。脚本会对 `knowledgeBaseIds` 逐库检索并合并结果。
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
cd "${HOME}/.agents/skills/ai-localbase"
cp .env.example .env
# 编辑 .env 填入实际配置
```

**2. Linux / Bash 使用方式**
```bash
# 使用实际运行目录下的 skill 脚本

# 初始化前可显式查看工具能力和已有知识库
"${HOME}/.agents/skills/ai-localbase/ai-localbase.sh" tools
"${HOME}/.agents/skills/ai-localbase/ai-localbase.sh" call "document.list" '{"knowledgeBaseId":"kb-1"}'
"${HOME}/.agents/skills/ai-localbase/ai-localbase.sh" list

# 初始化目录到知识库的映射，`/www/agents` 会映射为知识库名 `agents`
"${HOME}/.agents/skills/ai-localbase/ai-localbase.sh" init "/path/to/project"

# 上传文档（参数：文件名、内容、目录）
"${HOME}/.agents/skills/ai-localbase/ai-localbase.sh" upload "my-doc.md" "# 内容" "/path/to/project"

# 追加文档内容（参数：documentId、内容、目录）
"${HOME}/.agents/skills/ai-localbase/ai-localbase.sh" append "doc-123" "追加内容" "/path/to/project"

# 覆盖文档内容（参数：documentId、内容、目录）
"${HOME}/.agents/skills/ai-localbase/ai-localbase.sh" update "doc-123" "# 全量新内容" "/path/to/project"

# 删除文档（参数：documentId、目录）
"${HOME}/.agents/skills/ai-localbase/ai-localbase.sh" delete "doc-123" "/path/to/project"

# 检索内容（参数：关键词、目录）
"${HOME}/.agents/skills/ai-localbase/ai-localbase.sh" search "关键词" "/path/to/project"

# 检索内容并限制返回数量（参数：关键词、目录、topK）
"${HOME}/.agents/skills/ai-localbase/ai-localbase.sh" search "关键词" "/path/to/project" 5

# 问答（参数：问题、目录）
"${HOME}/.agents/skills/ai-localbase/ai-localbase.sh" chat "你的问题" "/path/to/project"
```

**3. macOS 使用方式**

从任意工作目录调用，项目参数仍传会话启动目录。`bash` 显式启动可避免脚本缺少执行权限时报 `Permission denied`：

```bash
MACOS_LOCALBASE="${HOME}/.agents/skills/ai-localbase/ai-localbase-macos.sh"
bash "$MACOS_LOCALBASE" tools
bash "$MACOS_LOCALBASE" list
bash "$MACOS_LOCALBASE" init "/path/to/project"
bash "$MACOS_LOCALBASE" search "关键词" "/path/to/project" 5
bash "$MACOS_LOCALBASE" call document.detail '{"knowledgeBaseId":"kb-1","documentId":"doc-1"}'
```

`upload`、`append`、`update`、`delete`、`chat` 参数与 Linux 入口相同，沿用既有写入授权和响应验收规则。macOS 入口先去除 CRLF 行尾回车，再通过 `source /dev/stdin` 加载配置，避开当前 Bash 3.2 下 `source <(...)` 静默漏读的问题；配置文件不存在、必填变量为空或配置语法错误时仍失败，不把错误当作加载成功。

**4. PowerShell 使用方式**
```powershell
# 初始化目录到知识库的映射，`C:\work\agents` 会映射为知识库名 `agents`
& "$HOME/.agents/skills/ai-localbase/ai-localbase.ps1" tools
& "$HOME/.agents/skills/ai-localbase/ai-localbase.ps1" call "document.list" '{"knowledgeBaseId":"kb-1"}'
& "$HOME/.agents/skills/ai-localbase/ai-localbase.ps1" list
& "$HOME/.agents/skills/ai-localbase/ai-localbase.ps1" init "C:\work\project"

# 上传文档
& "$HOME/.agents/skills/ai-localbase/ai-localbase.ps1" upload "my-doc.md" "# 内容" "C:\work\project"
& "$HOME/.agents/skills/ai-localbase/ai-localbase.ps1" upload-file "my-doc.md" "C:\work\project\docs\my-doc.md" "C:\work\project"

# 追加文档内容
& "$HOME/.agents/skills/ai-localbase/ai-localbase.ps1" append "doc-123" "追加内容" "C:\work\project"
& "$HOME/.agents/skills/ai-localbase/ai-localbase.ps1" append-file "doc-123" "C:\work\project\docs\append.md" "C:\work\project"
& "$HOME/.agents/skills/ai-localbase/ai-localbase.ps1" append "doc-123" "@C:\work\project\docs\append.md" "C:\work\project"

# 覆盖文档内容
& "$HOME/.agents/skills/ai-localbase/ai-localbase.ps1" update "doc-123" "# 全量新内容" "C:\work\project"
& "$HOME/.agents/skills/ai-localbase/ai-localbase.ps1" update-file "doc-123" "C:\work\project\docs\full.md" "C:\work\project"

# 删除文档
& "$HOME/.agents/skills/ai-localbase/ai-localbase.ps1" delete "doc-123" "C:\work\project"

# 检索
& "$HOME/.agents/skills/ai-localbase/ai-localbase.ps1" search "关键词" "C:\work\project"

# 问答
& "$HOME/.agents/skills/ai-localbase/ai-localbase.ps1" chat "你的问题" "C:\work\project"
```

统一入口会自动：
- 加载 `.env` 配置
- 将 `docs/[任务目录]` 类入参归一到项目启动目录
- 先调用 `tools/list` 罗列工具能力，再调用 `knowledge_base.list` 检索已有知识库
- 按项目启动目录 basename、相似名称前缀和描述路径匹配知识库，匹配成功则刷新 `knowledge.json`
- 未匹配到已有知识库时才调用 `knowledge_base.create`
- 执行对应操作

## 使用流程

1. **先进入实际运行目录**：所有命令、`.env`、`knowledge.json` 都在 `${HOME}/.agents/skills/ai-localbase/` 目录下
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
| macOS 报 `MCP_API_BASE_URL` 缺失 | 先确认使用 `bash ai-localbase-macos.sh`；旧 Bash 入口的进程替换加载可能漏读配置。新入口仍报错时再检查配置，不因这条提示直接认定服务端故障 |
| `knowledgeBaseId` 为空 | 先执行 `list` 确认是否存在 `name == 当前项目目录名` 的知识库，再重新执行 `init` |
| 不知道 `documentId` | `upload` 的返回结果里会带文档 ID；`search` 命中结果里也会带 `documentId` |
| 无法上传目录 | 需客户端先遍历目录，再逐文件调用 `upload` |
| 搜索返回空 | 确认文档已上传并完成索引，尝试缩小 `query` 或增大 `topK` |
| 文本里有引号或换行 | Bash / PowerShell 入口都已处理转义，直接通过脚本传参即可 |
