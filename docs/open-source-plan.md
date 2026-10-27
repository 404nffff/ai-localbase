# AI LocalBase 开源计划（Open Source Plan）

## 目标

- 在不泄露本地数据与敏感配置的前提下完成开源发布。
- 让外部开发者可以在 10 分钟内跑通基础链路（上传文档 → 检索问答）。
- 建立最小可维护的社区协作机制（Issue、PR、版本发布）。

---

## 发布范围（Scope）

### 本次开源包含

- 后端服务代码（Go + Gin + Qdrant 集成）
- 前端界面代码（React + Vite）
- Docker 与 Compose 配置
- 基础文档与开发计划文档
- 测试代码（单元测试 + E2E）

### 本次开源不包含

- `backend/data/uploads/` 中任何用户上传文件
- `backend/data/app-state.json` 中任何本地模型地址与 key 信息
- 本地实验性临时文件（构建产物、二进制、日志）

---

## 开源前检查清单（发布阻断项）

- [x] 1. 仓库清理
  - [x] 删除/确认忽略所有本地数据文件与二进制
  - [x] 校验 [`.gitignore`](../.gitignore) 覆盖 `backend/data/**`、`frontend/node_modules/`、`qdrant_storage/`
- [x] 2. 敏感信息审计
  - [x] 全仓搜索 `apiKey`、`token`、`password`、`localhost` 私有地址
  - [x] 确认没有真实密钥、个人路径、私有 URL（排除 `node_modules`、`dist`、`backend/data` 后未发现泄露）
- [x] 3. License 与法律信息
  - [x] 添加 `LICENSE`（MIT）
  - [x] 在 [`README.md`](../README.md) 中补充 License 章节与协作入口
  - [ ] 检查第三方依赖许可兼容性（Go / npm）
- [x] 4. 文档就绪
  - [x] README 快速开始可直接执行
  - [x] 添加 `CONTRIBUTING.md`（提交规范、分支策略、PR 模板）
  - [x] 添加 `SECURITY.md`（漏洞报告渠道）
  - [x] 添加 `CHANGELOG.md`（v0.1.0 首版）
- [x] 5. 质量门禁
  - [x] 后端测试通过：`go test ./...`
  - [x] 前端构建通过：`npm run build`
  - [x] Docker 构建验证：按发布策略设为可选，不作为阻断项

### 执行记录（2026-03-14）

- ✅ `go test ./...`（backend）通过
- ✅ `npm run build`（frontend）通过
- ℹ️ `docker compose build` 未纳入本次开源阻断条件（按维护者决策）
- [ ] 6. 发布准备
  - [ ] 打版本 Tag：`v0.1.0`
  - [ ] 发布说明（Release Notes）
  - [ ] 创建基础 Issue 模板（Bug/Feature）

---

## 发布步骤（执行顺序）

1. **冻结发布分支**
   - 从主分支切 `release/v0.1.0`。
2. **执行清理与审计**
   - 跑一轮敏感词扫描与 `.gitignore` 校验。
3. **补齐开源元文件**
   - 补充 `LICENSE`、`CONTRIBUTING.md`、`SECURITY.md`、`CHANGELOG.md`。
4. **执行质量门禁**
   - 本地与 CI 通过所有测试与构建。
5. **创建首个公开版本**
   - 合并到主分支，打 Tag `v0.1.0`，生成 Release Notes。
6. **发布后观测**
   - 72 小时内监控 Issues，优先处理启动失败与文档问题。

---

## 角色分工建议

- **Maintainer（你）**：版本发布、路线决策、PR 最终合并。
- **Reviewer**：代码审查、测试门禁、文档校对。
- **Community 维护**：Issue 分流、FAQ 汇总。

---

## 风险与应对

- **风险 1：泄露本地敏感配置**
  - 应对：发布前执行敏感词扫描，默认不提交 `backend/data/**`。
- **风险 2：新用户跑不起来**
  - 应对：保证 [`README.md`](../README.md) 最短路径可执行，并提供常见错误排查。
- **风险 3：Issue 量突增**
  - 应对：先发布 MVP 范围，使用模板收敛问题描述质量。

---

## 首版开源定义（Definition of Done）

满足以下条件即可视为“开源就绪”：

- `v0.1.0` Tag 已发布
- `README + LICENSE + CONTRIBUTING + SECURITY + CHANGELOG` 齐全
- 本地与 CI 测试通过（当前本地后端测试与前端构建已通过）
- 无敏感信息泄露
- 新用户可按文档完成端到端最小验证
