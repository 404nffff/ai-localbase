# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

AI LocalBase is a local-first RAG knowledge base system. Users upload documents (TXT, MD, PDF, XLSX, CSV), which get chunked and embedded into Qdrant, then chat with an LLM using retrieved context. Backend is Go (Gin), frontend is React+TypeScript (Vite), vector DB is Qdrant, chat history is SQLite (pure Go driver, no CGO).

## Windows 本地开发方案（无 Docker）

前置条件：本机已安装 Go、Node.js、Qdrant。

### 1. 启动 Qdrant

确保 Qdrant 在 `http://localhost:6333` 运行（本机已安装）。

### 2. 启动后端

```bash
cd backend
go run .
```

后端默认监听 `:8080`，数据文件写入 `backend/data/` 目录（首次运行自动创建）。

可选环境变量（通过 shell export 或 IDE 配置）：

```bash
export QDRANT_URL=http://localhost:6333
export OLLAMA_BASE_URL=http://localhost:11434
export PORT=8080
```

完整配置项见 `backend/internal/config/config.go`，所有配置均通过环境变量注入，默认值可直接使用。

### 3. 启动前端

```bash
cd frontend
npm install
npm run dev
```

Vite 开发服务器运行在 `:5173`，默认已配置代理将 `/api`、`/v1`、`/health`、`/upload`、`/mcp` 转发到 `http://localhost:8080`。

### 4. 访问应用

浏览器打开 `http://localhost:5173`。

## Build Commands

```bash
# Backend
cd backend
go run .                        # 开发运行
go build -o ai-localbase .      # 编译二进制
go test ./...                   # 运行所有测试

# Frontend
cd frontend
npm install                     # 安装依赖
npm run dev                     # 开发服务器 (:5173)
npm run build                   # 生产构建 → dist/
npm run lint                    # ESLint 检查
npm run preview                 # 预览生产构建

# Windows 单 exe 构建（前端嵌入 Go 二进制）
build.bat                       # 一键构建：npm build → copy → go build
# 产物：ai-localbase.exe，运行后访问 http://localhost:8080
```

详细说明见 `docs/windows-build.md`。

## Architecture

**开发模式**（前后端分离）：

```
Browser (:5173) ──Vite proxy──> Backend (:8080, Gin)
                                   │
                       ┌───────────┼───────────────┐
                       ▼           ▼               ▼
                 Qdrant (:6333)  Ollama (:11434)  SQLite (文件)
```

**单 exe 模式**（前端嵌入 Go 二进制）：

```
Browser ──HTTP/SSE──> ai-localbase.exe (:8080)
                         │  ├── /api/*, /v1/*, /mcp/* → 业务逻辑
                         │  └── /* → 内嵌 React 前端（//go:embed）
                         │
             ┌───────────┼───────────────┐
             ▼           ▼               ▼
       Qdrant (:6333)  Ollama (:11434)  SQLite (文件)
```

### Backend（Go, `backend/`）

- **入口** `main.go`：按顺序创建 Config → QdrantService → AppStateStore → ChatHistoryStore → AppService → LLMService → MCP Server → Router
- **`internal/handler/`**：Gin HTTP handlers，处理所有 REST API
- **`internal/service/app_service.go`**：核心业务逻辑，串联所有服务
- **`internal/service/rag_service.go`**：分块、嵌入、上下文构建
- **`internal/service/llm_service.go`**：LLM 调用（支持 OpenAI 兼容 API 和 Ollama 原生 API，流式/非流式）
- **`internal/service/qdrant_service.go`**：Qdrant HTTP 客户端（原始 HTTP，无 SDK）
- **`internal/service/app_state_store.go`**：JSON 文件持久化（原子写入：先写临时文件再 rename）
- **`internal/service/chat_history_store.go`**：SQLite 聊天记录存储
- **`internal/mcp/`**：内置 MCP JSON-RPC 服务器，12 个工具（read/write/danger 三级权限）
- **`internal/util/document_text.go`**：文档文本提取和分块逻辑
- **`internal/model/types.go`**：所有数据模型定义

### Frontend（React, `frontend/src/`）

- **`App.tsx`**：根组件，所有状态管理和 API 调用集中在此
- **`components/ChatArea.tsx`**：聊天界面，SSE 流式响应处理
- **`components/Sidebar.tsx`**：会话列表、知识库选择、设置入口
- **`components/knowledge/KnowledgePanel.tsx`**：知识库 CRUD + 文件上传（含目录上传）
- **`components/settings/SettingsPanel.tsx`**：聊天 + 嵌入 + MCP 配置面板

### 文档处理流水线

```
文件上传 → ExtractDocumentText() → ChunkText()（语义分块） → BuildDocumentChunks()
→ EmbedTexts()（批量嵌入，LRU 缓存） → UpsertPoints()（批量写入 Qdrant）
```

PDF 提取优先使用 `pdftotext`（poppler），回退到 Go 库。Windows 上建议安装 poppler 以获得更好的中文 PDF 支持。

### RAG 检索流水线

```
用户查询 → [可选] LLM 查询改写 → 查询嵌入 → [可选] 语义缓存
→ Qdrant 检索 → 重排序 → MMR 多样性选择 → 去重 → 上下文裁剪
→ [可选] LLM 上下文压缩 → 注入 LLM prompt → 生成回答
```

### 数据持久化

- **向量数据**：Qdrant（`http://localhost:6333`）
- **聊天记录**：SQLite（`backend/data/chat-history.db`）
- **应用状态**：JSON 文件（`backend/data/app-state.json`）
- **上传文件**：本地文件系统（`backend/data/uploads/`）

## Key Design Decisions

- **Qdrant 使用原始 HTTP 客户端**，不依赖 Qdrant SDK，通过 `net/http` 直接调用 REST API
- **SQLite 使用 `modernc.org/sqlite`**（纯 Go 实现），不需要 CGO，交叉编译无障碍
- **没有消息队列、gRPC、Redis**，所有请求在单个 Go 进程内同步处理
- **模型调用有优先级队列**（`model_runtime_gate.go`），防止 Ollama 过载
- **MCP 工具分三级权限**：read（只读）、write（写入）、danger（删除，需确认头）

## Agent 格式规范

- 使用 `##` / `###` 标题区分主要章节与子章节
- 关键结论使用 **加粗** 标注
- 步骤类内容用有序列表，并列要点用无序列表
- 不同主题之间用 `---` 分隔线隔开
- 代码始终用代码块包裹并标注语言类型
- 始终使用简体中文回复
