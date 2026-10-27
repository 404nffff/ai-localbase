# AI LocalBase - 开发计划文档

## 项目概述

**项目名称**: AI LocalBase  
**项目类型**: 本地 AI 知识库系统（RAG）  
**技术栈**: Go + React + Qdrant + Docker  
**主要特性**: 兼容 OpenAI API，同时原生支持 Ollama（`/api/chat`、`/api/embed`）

---

## 技术架构

### 系统架构图

```text
[前端 React/Vite] ←→ [后端 Go/Gin] ←→ [Qdrant 向量数据库]
      ↓                      ↓
 [用户界面]           [文档处理流水线]
      ↓                      ↓
 [配置管理]           [AI API 调用层]
                             ↓
                   [Ollama 原生 API / OpenAI 兼容 API]
```

### 核心组件

- **前端**: React + TypeScript，ChatGPT 风格界面（侧边栏 + 聊天页 + 设置页）
- **后端**: Go + Gin，REST API + SSE
- **向量数据库**: Qdrant（集合生命周期管理 + 向量检索）
- **AI 集成**: Ollama 原生接口 + OpenAI 兼容接口
- **部署**: Docker Compose（三服务：frontend + backend + qdrant）

---

## 里程碑状态（最新）

### MVP（Day 3）✅ 已达成

- [x] 基础文档上传和存储
- [x] 简单向量检索
- [x] 后端 API 可用

### Alpha（Day 5）✅ 已达成

- [x] 完整 RAG 闭环（上传→切分→嵌入→存储→检索→回答）
- [x] 前端聊天界面
- [x] Ollama 集成（原生 API + 真实环境验证）

### Beta（Day 6）✅ 已达成

- [x] 端到端测试通过（E2E + 单元测试）
- [x] 错误处理完善（重试 + 回退 + 降级）
- [x] 用户体验优化（流式输出、知识库/文档范围控制）

### Release（Day 7）🔄 进行中

- [x] Docker 部署就绪
- [x] 文档体系基本齐全
- [x] GitHub 开源计划文档（docs/open-source-plan.md）
- [x] 开源元文件补齐（LICENSE / CONTRIBUTING / SECURITY / CHANGELOG）
- [ ] GitHub 开源准备执行（剩余：Tag、Release Notes、Issue 模板）
- [ ] 启动辅助脚本
- [ ] 性能基线实测（上传时延 / 查询 P95）

---

## 详细任务分解

### 后端开发任务

#### 1. 基础框架（Day 1）

- [x] 初始化 Gin 路由
- [x] CORS 配置
- [x] 基础中间件
- [x] 健康检查 API

#### 2. Qdrant 集成（Day 1-2）

- [x] Qdrant 客户端连接
- [x] 集合创建与删除
- [x] 向量插入接口
- [x] 相似度搜索接口

#### 3. 文档处理（Day 2）

- [x] 文件上传 API
- [x] 支持格式：TXT / MD / PDF
- [x] 文本提取与清洗（PDF: `ledongthuc/pdf`）
- [x] 分块策略（800 字符窗口 + 120 重叠）

#### 4. 嵌入生成（Day 2-3）

- [x] 嵌入 API 集成（OpenAI 兼容 + Ollama 原生）
- [x] 批量分批调用（batch）
- [x] 指数退避重试
- [x] 嵌入缓存机制（内存 LRU，2048 条，线程安全）

#### 5. RAG 检索逻辑（Day 3）

- [x] 查询向量化
- [x] 候选召回 + 相关文档检索
- [x] 上下文组装
- [x] Prompt 模板

#### 6. AI API 代理（Day 4）

- [x] OpenAI 兼容聊天接口（非流式 + SSE 流式）
- [x] Ollama 原生接口（非流式 + 流式）
- [x] Chat + Embedding 独立配置
- [x] 请求转发与响应处理
- [x] LLM 失败降级回退（degraded metadata）

### 前端开发任务

#### 1. 基础框架（Day 1）

- [x] Vite + React + TypeScript
- [x] 路由与基础布局
- [x] 样式体系

#### 2. UI 组件（Day 4-5）

- [x] 侧边栏组件
- [x] 聊天界面组件
- [x] 知识库管理组件
- [x] 设置面板组件

#### 3. 状态管理（Day 5）

- [x] 聊天历史状态
- [x] 配置状态
- [x] 文档列表状态

#### 4. API 集成（Day 5-6）

- [x] 聊天接口集成（SSE）
- [x] 文件上传集成
- [x] 配置管理集成

### 部署与运维

#### 1. Docker 配置（Day 1, 7）

- [x] 后端 Dockerfile
- [x] 前端 Dockerfile
- [x] Qdrant 配置
- [x] Docker Compose（三服务编排）

#### 2. 环境配置（Day 7）

- [x] 环境变量管理（含 `STATE_FILE`）
- [x] 配置文档
- [ ] 启动脚本

---

## 检索命中优化（专项进展）

> 详见 `docs/retrieval-improvement-plan.md`

### 第一阶段（参数级优化）✅ 已完成

- [x] 动态 TopK（按范围切换参数）
- [x] 单文档限额（防止单文档霸榜）
- [x] 参数首版落地

### 第二阶段（重排与多样性）✅ 已完成首版

- [x] 候选重排 `rerankCandidates()`
- [x] MMR 去冗余 `selectWithMMR()`
- [x] 文档多样性控制（per-document limit）

### 第三阶段（兜底与可观测）✅ 已完成首版

- [x] 低置信度判定 `isLowConfidenceSelection()`
- [x] 二次扩召回兜底
- [x] 检索日志指标 `logRetrievalMetrics()`
- [ ] 可观测看板（后续）

### 检索专项测试

- [x] 新增 `app_service_retrieval_test.go`
  - `TestResolveRetrievalParams`
  - `TestSelectWithMMRRespectsPerDocumentLimit`
  - `TestRerankCandidatesBoostsKeywordCoverage`
  - `TestIsLowConfidenceSelection`

---

## 验收标准状态

### 功能验收

- [x] 文档上传（TXT/PDF/MD）
- [x] 基于文档内容回答问题
- [x] Ollama 本地模型配置与调用
- [x] ChatGPT 风格界面
- [x] Docker 一键部署

### 性能验收

- [ ] 文档上传 < 30 秒（需统一实测基线）
- [ ] 查询响应 < 5 秒（需统计 P95）
- [ ] 并发用户访问压测（待执行）

### 质量验收

- [x] 错误处理完善
- [x] 日志记录完整
- [x] 代码结构清晰
- [x] 文档齐全

---

## 当前状态（2026-03-14）

**总体进度：约 93%，已进入 Release 收尾阶段。**

### 已完成关键能力

- [x] 全链路 RAG 可用（含 PDF）
- [x] Ollama + OpenAI 双接口兼容
- [x] 1024 维嵌入已验证写入 Qdrant
- [x] 嵌入缓存（LRU 2048）
- [x] 检索命中优化第一版已落地并有测试覆盖
- [x] 文档体系更新（README/architecture/retrieval plan）

### Release 剩余事项

- [ ] GitHub 开源发布动作（Tag、Release Notes、Issue 模板）
- [ ] 启动辅助脚本
- [ ] 性能压测与指标固化
- [ ] 检索可观测看板（可选）

---

**最后更新**: 2026年3月14日
**版本**: v1.8
