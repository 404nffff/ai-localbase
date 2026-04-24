# Windows 开发与发布指南

## 本地开发（无需 Docker）

前置条件：本机已安装 Go、Node.js、Qdrant。

### 1. 启动 Qdrant

确保 Qdrant 在 `http://localhost:6333` 运行。

### 2. 启动后端

```bash
cd backend
go run .
```

后端默认监听 `:8080`，数据写入 `backend/data/`。

### 3. 启动前端

```bash
cd frontend
npm install
npm run dev
```

Vite 开发服务器运行在 `:5173`，已配置代理将 API 请求转发到 `:8080`。

### 4. 访问应用

浏览器打开 `http://localhost:5173`。

---

## 单 exe 构建

将前端构建产物嵌入 Go 二进制，输出一个独立的 `.exe` 文件。

### 一键构建

```bat
build.bat
```

构建脚本会依次执行：
1. `npm install` + `npm run build` — 构建前端到 `frontend/dist/`
2. 复制 `frontend/dist/` 到 `backend/dist/`
3. `go build -o ai-localbase.exe .` — 编译 Go 二进制（嵌入前端）

### 手动构建

```bash
# 构建前端
cd frontend
npm install
npm run build
cd ..

# 复制到 backend/dist
xcopy /E /Y /I frontend\dist backend\dist

# 编译 exe
cd backend
go build -o ..\ai-localbase.exe .
cd ..
```

### 运行

```bash
ai-localbase.exe
```

浏览器打开 `http://localhost:8080`，前端界面由 Go 后端直接提供。

### 可选环境变量

通过系统环境变量或命令行 `set` 配置：

```bat
set QDRANT_URL=http://localhost:6333
set OLLAMA_BASE_URL=http://localhost:11434
set PORT=8080
```

完整配置项见 `backend/internal/config/config.go`。

---

## 架构说明

单 exe 模式下，Go 后端同时承担两个职责：

- **API 服务**：`/api/*`、`/v1/*`、`/mcp/*` 等业务接口
- **静态文件服务**：通过 `//go:embed` 嵌入前端构建产物，使用 `NoRoute` 实现 SPA 路由回退

与 Docker 部署的对比：

| | Docker 部署 | 单 exe 部署 |
|---|---|---|
| 前端服务 | Nginx 容器 | Go 内嵌 `http.FileServer` |
| API 代理 | Nginx → Backend | 同进程，无代理 |
| 构建产物 | 3 个容器镜像 | 1 个 exe 文件 |
| 适用场景 | 服务器部署 | Windows 本地/便携分发 |

---

## 关键实现细节

### embed 工作原理

- `backend/embed.go` 使用 `//go:embed all:dist` 指令将 `backend/dist/` 目录嵌入二进制
- `frontendFS()` 函数在运行时判断：如果磁盘上有实际构建产物则使用 `os.DirFS`（开发调试用），否则使用嵌入的 `embed.FS`
- `backend/dist/.gitkeep` 是占位文件，确保无前端构建产物时也能编译通过

### SPA 路由回退

`router.go` 中的 `spaHandler` 实现了与 Nginx `try_files $uri $uri/ /index.html` 等价的行为：
1. 请求路径匹配到静态文件（如 `/assets/index.js`）→ 直接返回
2. 请求路径不匹配 → 返回 `index.html`，由 React 处理客户端路由
