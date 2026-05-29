# 故障排查指南

本文档提供 API LocalBase 在本地开发和 Docker 部署中常见问题的诊断与解决方案。

---

## 1. Docker + Ollama 本地集成

### 前置条件

本地 Ollama 服务必须先可用，再通过 Docker 调用。

#### 1.1 确认 Ollama 服务正常

执行以下命令验证：

```bash
# 列出已下载的模型
ollama list

# 拉取需要的模型（如无则执行）
ollama pull qwen3.5:0.8b
ollama pull nomic-embed-text

# 启动 Ollama 服务（或确保已在后台运行）
ollama serve --host 0.0.0.0 --port 11434
```

验证接口可用：

```bash
curl -v http://localhost:11434/v1/models
```

- 成功：返回 `HTTP/1.1 200 OK` 及模型列表 JSON
- 失败：若返回 `no configuration file provided`、`404`、`5xx`，说明 Ollama 服务未正确启动，需先修复 Ollama

---

### 1.2 Docker 容器无法访问 Ollama

**症状**：
- 容器内 curl 失败：`Failed to resolve host: host.docker.internal`
- 或返回 `no configuration file provided: not found`（Ollama 部分可到达但不是 API 模式）

**原因**：
- 容器内 `host.docker.internal` DNS 未映射
- Ollama 服务未以 HTTP API 模式启动

**解决方案**：

在 `docker-compose.yml` 的 `backend` 服务中添加 `extra_hosts`：

```yaml
services:
  backend:
    ...
    extra_hosts:
      - "host.docker.internal:host-gateway"
    environment:
      OLLAMA_BASE_URL: http://host.docker.internal:11434
```

然后重启：

```bash
docker compose down
docker compose up -d --build
```

验证（容器内）：

```bash
docker compose exec backend sh -c "apk add --no-cache curl && curl -v http://host.docker.internal:11434/v1/models"
```

- 成功：返回模型列表 JSON
- 失败：检查本地 Ollama 是否用 `ollama serve` 启动

---

### 1.3 向量维度不匹配

**症状**：
- 聊天或知识库查询报错：`Vector dimension error: expected dim: 768, got 1024`

**原因**：
- Qdrant 集合初始化时的向量维度与 Embedding 模型的实际输出维度不一致
- 通常是旧数据与新模型配置冲突

**解决方案**：

1. **确认 Embedding 模型的向量维度**：
   ```bash
   ollama list
   # 查看 embedding 模型名称，比如 nomic-embed-text（通常 768）、bge-m3（通常 1024）
   ```

2. **修改 `.env` 的 `QDRANT_VECTOR_SIZE`**：
   ```bash
   cp .env.example .env
   # 根据 embedding 模型维度修改
   QDRANT_VECTOR_SIZE=768
   ```

3. **清理旧数据或切换集合前缀并重启**（重要，同一个 collection 不能混用不同向量维度）：
   ```bash
   # 方案 A：确认不需要旧数据后清理
   docker compose down
   rm -rf qdrant_storage backend/data/app-state.json
   docker compose up -d --build

   # 方案 B：保留旧数据，使用新集合前缀
   # 在 .env 中设置：QDRANT_COLLECTION_PREFIX=kb_768_
   docker compose up -d --build
   ```

4. **验证后端配置**：
   ```bash
   curl -s http://localhost:8080/api/config | jq .embedding
   ```
   确保 `baseUrl` 和 `model` 与 Ollama 中可用的一致。

---

### 1.4 模型调用失败

**症状**：
- 前端聊天报错：`模型调用失败` 或 `AI 模型调用失败`
- 后端日志显示：`model not found` 或 API 连接错误

**原因**：
- 配置中指定的模型在 Ollama 中不存在
- 模型名称拼写错误
- Ollama 服务不可达

**解决方案**：

1. **检查 Ollama 已有模型**：
   ```bash
   ollama list
   ```

2. **验证后端配置与 Ollama 模型一致**：
   ```bash
   curl -s http://localhost:8080/api/config | jq '.chat.model, .embedding.model'
   ```

3. **若模型未下载，执行 pull**：
   ```bash
   ollama pull qwen3.5:0.8b
   ollama pull nomic-embed-text
   ```

4. **若模型名称错误，在 UI 设置中修改**，或直接 PUT `/api/config`：
   ```bash
   curl -X PUT http://localhost:8080/api/config \
     -H "Content-Type: application/json" \
     -d '{
       "chat": {
         "provider": "ollama",
         "baseUrl": "http://host.docker.internal:11434",
         "model": "qwen3.5:0.8b",
         "apiKey": "",
         "temperature": 0.7,
         "contextMessageLimit": 12
       },
       "embedding": {
         "provider": "ollama",
         "baseUrl": "http://host.docker.internal:11434",
         "model": "nomic-embed-text",
         "apiKey": ""
       }
     }'
   ```

5. **重启后端**：
   ```bash
   docker compose restart backend
   ```

---

### 1.5 知识库初始化失败

**症状**：
- 前端显示：`知识库初始化失败：请求失败`
- 或提示 `ERR_EMPTY_RESPONSE`

**原因**：
- 后端 API 未正确启动或无响应
- 前端代理配置不正确（多见于 Preview 模式）
- Qdrant 未就位

**解决方案**：

1. **检查后端是否启动**：
   ```bash
   curl -s http://localhost:8080/api/knowledge-bases | jq .
   ```
   应返回知识库列表 JSON。

2. **检查 Qdrant 是否启动**：
   ```bash
   curl -s http://localhost:6333/collections | jq .
   ```
   应返回集合列表或空列表。

3. **检查前端是否正确代理到后端**（Nginx 配置）：
   ```bash
   curl -v http://localhost:4173/api/knowledge-bases
   ```
   应返回后端的相同响应。

4. **重启整个服务栈**：
   ```bash
   docker compose down
   docker compose up -d --build
   sleep 5
   curl -s http://localhost:8080/api/knowledge-bases | jq .
   ```

---

## 2. 本地开发模式

### 2.1 后端启动失败

**症状**：
- 执行 `go run ./` 报错：`module not found` 或编译错误

**原因**：
- 依赖未下载
- Go 版本不符

**解决方案**：

```bash
cd backend
go mod download
go mod tidy
go run ./
```

确保 Go 版本 >= 1.21：
```bash
go version
```

---

### 2.2 前端启动失败

**症状**：
- `npm run dev` 失败或访问 http://localhost:3000 无响应

**原因**：
- 依赖未安装
- Node 版本不符

**解决方案**：

```bash
cd frontend
npm install
npm run dev
```

确保 Node 版本 >= 18：
```bash
node --version
```

---

### 2.3 前端代理配置

**本地启动前后端时**，前端需要正确代理后端 API。

编辑 `frontend/vite.config.ts`：

```typescript
export default defineConfig({
  plugins: [react()],
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/v1': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    }
  }
})
```

---

## 3. 常见错误信息及解决

| 错误信息 | 原因 | 解决方案 |
|---------|------|--------|
| `Vector dimension error: expected dim: 768, got 1024` | Qdrant 与 Embedding 维度不一致 | 改 `QDRANT_VECTOR_SIZE` 或 embedding 模型，清卷重启 |
| `no configuration file provided: not found` | Ollama API 服务不可用 | 执行 `ollama serve` 启动 Ollama |
| `Could not resolve host: host.docker.internal` | 容器 DNS 映射失败 | 在 `docker-compose.yml` 加 `extra_hosts` |
| `model not found` / `404` | 模型在 Ollama 中不存在 | 执行 `ollama pull <model>` 下载 |
| `connection refused` | 后端/Qdrant/Ollama 服务未启动 | 检查对应服务是否运行 |
| `ERR_EMPTY_RESPONSE` | 后端未准备好或前端代理配置错误 | 检查后端启动成功，检查前端代理配置 |

---

## 4. 完整重启流程（推荐）

当遇到不确定的问题时，执行完整重启可解决大多数环境问题：

```bash
# 1. 停止所有服务
docker compose down

# 2. 清理旧数据和配置
rm -rf qdrant_storage backend/data/app-state.json

# 3. 强制重建镜像
docker compose up -d --build

# 4. 等待启动完成
sleep 5

# 5. 验证
curl -s http://localhost:8080/api/config | jq .
curl -s http://localhost:4173/ | head -20
```

---

## 5. 调试命令速查

### 后端日志
```bash
docker compose logs backend --tail 50
```

### 前端日志（容器内）
```bash
docker compose logs frontend --tail 50
```

### Qdrant 日志
```bash
docker compose logs qdrant --tail 50
```

### 容器内网络诊断
```bash
docker compose exec backend sh -c "apk add --no-cache curl && curl -v http://host.docker.internal:11434/v1/models"
```

### 后端配置检查
```bash
curl -s http://localhost:8080/api/config | jq .
```

### 知识库列表
```bash
curl -s http://localhost:8080/api/knowledge-bases | jq .
```

### Qdrant 健康检查
```bash
curl -s http://localhost:6333/health | jq .
```

---

## 6. 获取帮助

如果问题仍无法解决，请收集以下信息：

1. 完整的错误日志：
   ```bash
   docker compose logs --all --tail 100 > logs.txt
   ```

2. 后端配置：
   ```bash
   curl -s http://localhost:8080/api/config > config.json
   ```

3. 环境信息：
   ```bash
   docker --version
   docker compose version
   go version
   node --version
   npm --version
   ollama --version
   ```

4. 详细的复现步骤

将这些信息提供给项目 Issue 或讨论区，可加快问题定位。

---

## 更新日志

- **2026-03-25**：初版，涵盖 Docker + Ollama 集成、本地开发、常见错误诊断
