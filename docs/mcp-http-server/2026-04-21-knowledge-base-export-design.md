# AI LocalBase 知识库导出设计稿

> 日期：2026-04-21  
> 执行者：Codex

## 目标

为 ai-localbase 增加一个**仅后端可调用**的知识库导出能力。该能力面向单个知识库，调用后直接返回 `application/zip` 文件流。导出内容不再使用原始上传文件，而是使用上传阶段额外沉淀到当前上传根目录下 `md/` 子目录中的标准化 Markdown 归档文件；若当前上传目录映射为 `upload/`，则对应路径即为 `upload/md/`。

## 用户已确认边界

1. 导出格式固定为 `zip`
2. 只做后端 API，前端暂不增加入口
3. 导出接口采用同步下载，直接返回 zip 文件流
4. 导出内容仅包含知识库元数据与 Markdown 归档文件
5. 上传新文档时额外保留一份 Markdown 归档到 `upload/md/`
6. 历史文档不做补齐迁移
7. 若归档文件缺失，导出不整体失败，而是在 `manifest.json` 中标记未导出原因

## 设计结论

### 总体方案

采用 **上传阶段生成 Markdown 归档 + 导出阶段只做打包** 的结构：

1. 继续保留现有原始上传文件保存逻辑
2. 在文档文本抽取完成后，同步写入一份 Markdown 归档文件到 `UploadDir/md/<knowledgeBaseId>/`
3. 在 `Document` 元数据中新增 Markdown 归档路径字段
4. 新增 `GET /api/knowledge-bases/:id/export` 导出接口
5. 导出时按文档记录的 Markdown 归档路径收集文件，流式写入 zip
6. zip 内固定输出 `manifest.json` 与 `documents/*.md`

这个方案的核心价值是：**上传时沉淀统一归档，导出时不再做格式转换，从而降低导出链路的失败点，并为未来导入能力保留稳定输入。**

### 为什么不在导出时再临时转换

如果继续沿用原始上传文件，在导出时再读取 `.pdf/.txt/.csv/.xlsx` 并临时转换为 Markdown，会带来以下问题：

- 导出耗时与失败点明显增加
- 相同文档每次导出结果可能受解析链路波动影响
- 导出接口会同时承担“解析 + 打包”两类职责
- 后续若要做导入，缺少稳定的归档源文件

因此，本次设计明确把“生成标准化 Markdown”放到上传阶段完成。

## 方案对比

### 方案 A：上传时生成 Markdown 归档，导出只打包归档文件

- 优点：符合已确认需求，上传与导出职责清晰
- 优点：后续做导入、迁移、MCP 导出时可直接复用归档文件
- 优点：缺失历史数据的行为边界清楚，不需要额外迁移脚本
- 缺点：旧文档在未重新上传前不会具备导出内容

### 方案 B：上传时不落盘，导出时再临时转换

- 优点：上传链路改动较少
- 缺点：与“上传时保留一份到 `upload/md/`”的需求冲突
- 缺点：导出阶段承担额外解析风险

### 方案 C：直接以 Markdown 归档替代原始文件作为主存储

- 优点：长期最统一
- 缺点：会影响现有上传、重建索引、删除文档等多处行为
- 缺点：改动面超出本次需求

结论：采用 **方案 A**。

## 文件设计

### 预计修改文件

1. `backend/internal/model/types.go`
2. `backend/internal/service/app_service.go`
3. `backend/internal/handler/app_handler.go`
4. `backend/internal/router/router.go`
5. `backend/internal/service/rag_service_test.go`
6. `backend/internal/router/router_e2e_test.go`
7. `docs/mcp-http-server/01-施工文档.md`
8. `docs/mcp-http-server/status.md`

### 高危预警

- **高危公共改动：`backend/internal/router/router.go`**
- **高危公共结构改动：`backend/internal/model/types.go`**

原因：

- `router.go` 是所有公开 API 的统一入口，新增导出路由会影响整体协议面
- `types.go` 中的 `Document` 属于后端和前端共享的数据结构，新增字段必须保持兼容，不得破坏已有 JSON 字段

## 数据结构设计

### Document 新增字段

在 `backend/internal/model/types.go` 的 `Document` 中新增字段：

```go
MarkdownPath string `json:"markdownPath"`
```

约束如下：

1. 新上传文档与后续内容覆写后的文档都会维护该字段
2. 历史文档字段为空，保持现状
3. 导出时只依赖该字段，不再反查原始上传文件
4. 该字段只保存服务端归档相对/可控路径，不写入导出 manifest 的宿主机绝对路径

## 上传链路设计

### 现状

当前上传流程为：

1. `AppHandler.handleUpload` 保存原始文件到 `serverConfig.UploadDir`
2. 构造 `Document`
3. `AppService.IndexDocument(document)` 抽取文本、切块、写向量库
4. `AddDocument` 把文档记录写入应用状态

### 新流程

上传流程调整为：

1. 继续保存原始文件到现有上传目录
2. `IndexDocument` 抽取文本后，生成标准化 Markdown 内容
3. 将 Markdown 内容写入 `filepath.Join(serverConfig.UploadDir, "md", <knowledgeBaseId>, <documentId>.md)`
4. 把归档路径写回 `document.MarkdownPath`
5. 再继续后续切块、Embedding、Qdrant upsert 与状态落盘

### Markdown 归档规则

归档文件采用固定命名：

```text
<UploadDir>/md/<knowledgeBaseId>/<documentId>.md
```

若当前部署把原始上传目录映射为 `upload/`，则该路径对外表现为 `upload/md/<knowledgeBaseId>/<documentId>.md`。

归档内容规则：

1. 以 `ExtractDocumentText` 的抽取结果作为正文来源
2. 使用 UTF-8 编码写入
3. 文件正文允许追加少量固定头信息，至少包含：
   - 文档名称
   - 所属知识库 ID
   - 上传时间
4. 正文主体必须是可直接阅读与再次导入的 Markdown 文本

### 内容覆写同步

现有仓库已支持通过 MCP 工具对文本型文档执行 `document.append` 与 `document.update`。为了保证导出拿到的是当前最新内容，需要在 `RewriteDocumentContent` 中同步执行以下动作：

1. 覆写原始文件
2. 重新生成对应 Markdown 归档
3. 再继续重建索引与替换文档元数据

这样可以保证：

- 导出不再依赖原始二进制文件
- 同一个文档对应一个稳定的归档文件
- 文档追加或覆盖后，导出内容不会落后于最新状态
- 文档删除时也能按元数据追踪归档文件

## 导出 API 设计

### 路由

新增路由：

```http
GET /api/knowledge-bases/:id/export
```

挂载位置保持在现有 `/api/knowledge-bases` 分组中，沿用当前 Bearer Token 鉴权中间件。

### 响应

成功时返回：

```http
200 OK
Content-Type: application/zip
Content-Disposition: attachment; filename="<knowledge-base-name>-export.zip"
```

失败时返回：

- `404 Not Found`：知识库不存在
- `500 Internal Server Error`：zip 写出失败、manifest 编码失败或流式输出异常

导出时**不会**因为部分文档没有 Markdown 归档而返回失败。

## zip 结构设计

导出 zip 固定为：

```text
<knowledge-base-name>-export.zip
├── manifest.json
└── documents/
    ├── doc-1.md
    ├── doc-2.md
    └── ...
```

其中：

- `manifest.json` 记录知识库元数据、导出时间与文档导出结果
- `documents/` 只放实际成功导出的 Markdown 归档文件
- zip 内文件名使用 `document.id`，避免用户原始文件名冲突

## manifest 设计

`manifest.json` 至少包含以下结构：

```json
{
  "knowledgeBase": {
    "id": "kb-1",
    "name": "产品知识库",
    "description": "售前资料",
    "createdAt": "2026-04-21T00:00:00Z"
  },
  "exportedAt": "2026-04-21T10:00:00Z",
  "documents": [
    {
      "id": "doc-1",
      "name": "产品介绍.pdf",
      "uploadedAt": "2026-04-21T09:00:00Z",
      "status": "indexed",
      "size": 1024,
      "sizeLabel": "1.0 KB",
      "archivePath": "documents/doc-1.md",
      "exported": true,
      "reason": ""
    },
    {
      "id": "doc-2",
      "name": "旧文档.pdf",
      "uploadedAt": "2026-04-20T09:00:00Z",
      "status": "indexed",
      "size": 2048,
      "sizeLabel": "2.0 KB",
      "archivePath": "",
      "exported": false,
      "reason": "markdown archive not generated"
    }
  ]
}
```

约束如下：

1. `archivePath` 只写 zip 内相对路径
2. `exported` 标记该文档是否实际进入 zip
3. `reason` 用于说明未导出原因
4. manifest 不暴露宿主机绝对路径

推荐的未导出原因文案：

- `markdown archive not generated`
- `markdown archive missing on disk`

## 服务分层设计

### AppService 负责

1. 在 `IndexDocument` 与 `RewriteDocumentContent` 阶段同步维护 Markdown 归档
2. 提供导出知识库所需的快照信息
3. 统一判定文档是否可导出
4. 构造 manifest 所需的结构化数据

### AppHandler 负责

1. 处理 `GET /api/knowledge-bases/:id/export`
2. 设置下载响应头
3. 以流式方式写出 zip
4. 将 manifest 与归档文件按固定路径写入压缩包

这样可以保证：**业务规则留在 service，HTTP 输出细节留在 handler**。

## 错误处理设计

### 上传阶段

- 原始文件保存失败：维持现有 `500`
- 文本抽取失败：维持现有上传失败行为
- Markdown 归档写入失败：本次上传整体失败，避免 state 与归档不一致

### 导出阶段

- 知识库不存在：返回 `404`
- `markdownPath` 为空：跳过该文档并在 manifest 标记 `markdown archive not generated`
- `markdownPath` 指向的文件不存在：跳过该文档并在 manifest 标记 `markdown archive missing on disk`
- zip 写出中途失败：返回 `500`

## 测试设计

至少补以下测试：

1. 上传新文档后会生成 `<UploadDir>/md/<knowledgeBaseId>/<documentId>.md`
2. 新上传文档的 `Document.MarkdownPath` 会被写入状态
3. `GET /api/knowledge-bases/:id/export` 成功返回 zip 响应头
4. zip 中包含 `manifest.json` 与成功导出的 `documents/*.md`
5. 历史文档 `markdownPath` 为空时，zip 仍成功，manifest 标记 `markdown archive not generated`
6. `markdownPath` 存在但文件缺失时，zip 仍成功，manifest 标记 `markdown archive missing on disk`
7. 知识库不存在时返回 `404`
8. manifest 不包含宿主机绝对路径

## 风险与取舍

### 已接受取舍

1. 历史文档不补齐，因此首次上线后旧知识库导出内容可能不完整
2. 上传链路多了一次本地写盘，但换来后续导出稳定性
3. `Document` 新增字段会让前端拉到更多数据，但属于向后兼容扩展

### 当前未纳入范围

1. 导入 zip 恢复知识库
2. 前端知识库面板增加“导出”按钮
3. 为历史数据提供批量重建 Markdown 归档脚本

## 实施顺序建议

1. 扩展 `Document` 结构与导出 manifest 结构
2. 在 `IndexDocument` 与 `RewriteDocumentContent` 中新增 Markdown 归档同步逻辑
3. 新增导出 handler 与路由
4. 补 service/router 测试
5. 更新施工文档与 README

## 验收标准

1. 新上传文档完成后，磁盘上可见对应 `<UploadDir>/md/<knowledgeBaseId>/<documentId>.md`
2. 调用 `GET /api/knowledge-bases/:id/export` 可直接下载 zip
3. zip 内固定包含 `manifest.json`
4. zip 只包含 Markdown 归档文件，不包含原始上传文件
5. 历史文档或缺失归档不会让整个导出失败
6. manifest 中能明确识别哪些文档已导出、哪些未导出及原因
