# Contributing to AI LocalBase

感谢你愿意参与 AI LocalBase 的建设。

## 开发环境

1. 安装 Go 1.21+、Node.js 18+、Docker。
2. 启动 Qdrant：

```bash
docker compose -f docker-compose.qdrant.yml up -d
```

3. 启动后端：

```bash
cd backend
go run .
```

4. 启动前端：

```bash
cd frontend
npm install
npm run dev
```

## 分支与提交规范

- 分支命名建议：
  - `feat/<topic>`
  - `fix/<topic>`
  - `docs/<topic>`
- 提交建议使用 Conventional Commits：
  - `feat: ...`
  - `fix: ...`
  - `docs: ...`
  - `refactor: ...`
  - `test: ...`

## Pull Request 要求

提交 PR 前请确保：

- [ ] 后端测试通过：`cd backend && go test ./...`
- [ ] 前端可构建：`cd frontend && npm run build`
- [ ] 变更说明清晰，包含动机和影响范围
- [ ] 如涉及接口/行为变化，已更新文档

## Issue 反馈建议

建议提供以下信息：

- 运行环境（OS、Go、Node 版本）
- 复现步骤
- 实际行为与预期行为
- 错误日志（脱敏后）

## 代码风格

- 后端遵循 Go 社区风格与 `gofmt`
- 前端保持 TypeScript 类型完整性
- 避免提交本地数据与密钥文件
