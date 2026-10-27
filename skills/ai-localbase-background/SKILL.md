---
name: ai-localbase-background
description: Use when starting any conversation in a project that uses ai_localbase as its primary knowledge base and may require background queueing, async polling, or project-local sync state.
---

## 运行位置

以下内容是在说明 skill 自身的安装位置与项目运行状态目录，不是让你在项目目录里创建同名说明文件。

- skill 安装目录：`~/.agents/skills/ai-localbase-background/`
- 默认配置文件：`~/.agents/skills/ai-localbase-background/.env`
- 每个项目的运行状态目录：`<project>/docs/.ai-localbase-background/`
- 若误传 `docs/[任务目录]`、`docs/[任务目录]/onlyAI` 或其子目录，入口脚本会自动回退到项目启动目录，再确认知识库和运行状态目录

## 适用场景

- 需要把文档上传改成后台执行
- 需要先返回 `jobId`，稍后轮询状态
- 需要把 worker、日志、任务队列和结果按项目隔离
- 需要保持 `search / chat` 同步返回，不走后台队列
- 需要把知识库映射、worker 日志、任务队列和结果放在项目本地状态目录

## 当前范围

当前命令包括：

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

- 每次进入项目仍然先执行 `init`，用于确认当前目录对应的主 `knowledgeBaseId` 与绑定集合 `knowledgeBaseIds`
- `init` 必须先调用 `tools/list` 罗列工具能力，再调用 `knowledge_base.list` 检索已有知识库，并按精确名称、名称前缀和描述路径匹配项目绑定库；匹配成功后把主 `knowledgeBaseId` 继续写入项目状态目录 `knowledge.json` 顶层旧格式字段，并把多库绑定写入 `_bindings`，没有匹配时才调用 `knowledge_base.create`
- `knowledge.json` 只能作为本地映射缓存，不能只凭缓存跳过服务端 `knowledge_base.list`
- 所有子命令的目录参数都应传项目启动目录；误传任务目录时会自动归一到项目根目录，避免按任务目录创建知识库
- Bash 入口同时内置同步 `upload / append / update / delete / search / chat`
- `queue-*` 在发现 worker 未运行时会自动拉起后台 worker
- 自动拉起的 worker 在队列清空并空闲一小段时间后会自行退出
- `search / chat` 保持同步执行，不进后台队列，并对绑定的多个知识库 ID 逐库读取后合并返回
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

`list` 子命令调用普通 HTTP 工具 `knowledge_base.list`，用于检索已有知识库。返回结构在 `structuredContent.items[]` 中，每项包含：

- `id` / `knowledgeBaseId`：真实知识库 ID，例如 `kb-3`
- `name`：知识库名称，初始化时必须用它精确匹配项目启动目录 basename
- `description`：知识库描述
- `createdAt`：创建时间
- `documentCount`：文档数量

初始化和 `queue-*` 入队时只能把匹配出的 `id` / `knowledgeBaseId` 当作后续工具调用的真实 ID。`knowledgeBaseId` 表示主库，写入类操作只写主库；`knowledgeBaseIds` 表示绑定库集合，读取类操作跨绑定集合合并。禁止把目录名或知识库名直接当作 ID。

项目状态目录中的 `knowledge.json` 顶层保持旧格式兼容：

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
- `knowledgeBaseId` 为空表示跨知识库搜索；项目内默认先执行 `init`，再使用当前项目对应的真实 `knowledgeBaseIds` 逐库检索并合并结果
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
"${HOME}/.agents/skills/ai-localbase-background/ai-localbase-background.sh" init "/path/to/project"

# 初始化前可显式查看工具能力和已有知识库
"${HOME}/.agents/skills/ai-localbase-background/ai-localbase-background.sh" tools
"${HOME}/.agents/skills/ai-localbase-background/ai-localbase-background.sh" list

# 直接同步上传
"${HOME}/.agents/skills/ai-localbase-background/ai-localbase-background.sh" upload "notes.md" "# 内容" "/path/to/project"

# 直接同步追加
"${HOME}/.agents/skills/ai-localbase-background/ai-localbase-background.sh" append "doc-123" "追加内容" "/path/to/project"

# 直接同步覆盖
"${HOME}/.agents/skills/ai-localbase-background/ai-localbase-background.sh" update "doc-123" "# 新内容" "/path/to/project"

# 直接同步删除
"${HOME}/.agents/skills/ai-localbase-background/ai-localbase-background.sh" delete "doc-123" "/path/to/project"

# 写入上传任务；若 worker 未运行会自动拉起
"${HOME}/.agents/skills/ai-localbase-background/ai-localbase-background.sh" queue-upload "notes.md" "# 内容" "/path/to/project"

# 追加文档任务
"${HOME}/.agents/skills/ai-localbase-background/ai-localbase-background.sh" queue-append "doc-123" "追加内容" "/path/to/project"

# 覆盖文档任务
"${HOME}/.agents/skills/ai-localbase-background/ai-localbase-background.sh" queue-update "doc-123" "# 新内容" "/path/to/project"

# 删除文档任务
"${HOME}/.agents/skills/ai-localbase-background/ai-localbase-background.sh" queue-delete "doc-123" "/path/to/project"

# 查询任务状态
"${HOME}/.agents/skills/ai-localbase-background/ai-localbase-background.sh" job-status "job-123" "/path/to/project"

# 查询任务结果
"${HOME}/.agents/skills/ai-localbase-background/ai-localbase-background.sh" job-result "job-123" "/path/to/project"

# 查看 worker 日志
"${HOME}/.agents/skills/ai-localbase-background/ai-localbase-background.sh" worker-logs "/path/to/project" 100

# 同步检索与问答
"${HOME}/.agents/skills/ai-localbase-background/ai-localbase-background.sh" search "关键词" "/path/to/project"
"${HOME}/.agents/skills/ai-localbase-background/ai-localbase-background.sh" search "/path/to/project" "关键词" 5
"${HOME}/.agents/skills/ai-localbase-background/ai-localbase-background.sh" chat "你的问题" "/path/to/project"

# 停止 worker
"${HOME}/.agents/skills/ai-localbase-background/ai-localbase-background.sh" worker-stop "/path/to/project"
```

## PowerShell 示例

```powershell
& "$HOME/.agents/skills/ai-localbase-background/ai-localbase-background.ps1" init "C:\work\project"
& "$HOME/.agents/skills/ai-localbase-background/ai-localbase-background.ps1" tools
& "$HOME/.agents/skills/ai-localbase-background/ai-localbase-background.ps1" list
& "$HOME/.agents/skills/ai-localbase-background/ai-localbase-background.ps1" upload "notes.md" "# 内容" "C:\work\project"
& "$HOME/.agents/skills/ai-localbase-background/ai-localbase-background.ps1" append "doc-123" "追加内容" "C:\work\project"
& "$HOME/.agents/skills/ai-localbase-background/ai-localbase-background.ps1" update "doc-123" "# 新内容" "C:\work\project"
& "$HOME/.agents/skills/ai-localbase-background/ai-localbase-background.ps1" delete "doc-123" "C:\work\project"
& "$HOME/.agents/skills/ai-localbase-background/ai-localbase-background.ps1" queue-upload "notes.md" "# 内容" "C:\work\project"
& "$HOME/.agents/skills/ai-localbase-background/ai-localbase-background.ps1" queue-append "doc-123" "追加内容" "C:\work\project"
& "$HOME/.agents/skills/ai-localbase-background/ai-localbase-background.ps1" queue-update "doc-123" "# 新内容" "C:\work\project"
& "$HOME/.agents/skills/ai-localbase-background/ai-localbase-background.ps1" queue-delete "doc-123" "C:\work\project"
& "$HOME/.agents/skills/ai-localbase-background/ai-localbase-background.ps1" job-status "job-123" "C:\work\project"
& "$HOME/.agents/skills/ai-localbase-background/ai-localbase-background.ps1" job-result "job-123" "C:\work\project"
& "$HOME/.agents/skills/ai-localbase-background/ai-localbase-background.ps1" search "关键词" "C:\work\project"
& "$HOME/.agents/skills/ai-localbase-background/ai-localbase-background.ps1" chat "你的问题" "C:\work\project"
& "$HOME/.agents/skills/ai-localbase-background/ai-localbase-background.ps1" worker-stop "C:\work\project"
```

## 后台写入的额外约束

- 先按上述筛选规则确定内容和操作，再调用 `queue-*`；后台 worker 不负责语义筛选，也不会自动合并同一来源的重复任务。
- 同一文档尚未入队的多次整体修订，由当前会话合成最新正文后只入队一次；已经 queued/running 的任务先查询结果，不能直接编辑队列文件或再投递一份。独立 append 记录不能被最后一条覆盖。
- 同一 documentId 的写入顺序执行并核对上一项结果；`job-status`、`job-result` 显示成功后仍按上传规则回读远端文档。
- worker 只在工具返回合法业务结果且未报 `isError` 时标记 succeeded；该状态不代表全文回读或 Wiki 整理已经完成。失败、结果未知或进程中断时先核对远端，不能自动重放整批任务。
- 任务 payload、worker.log 和 results 都是运行状态，不能再次同步到知识库，避免循环上传。

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
