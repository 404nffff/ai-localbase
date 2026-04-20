#!/usr/bin/env bash
set -euo pipefail

# 统一解析仓库根目录，避免从其他目录调用时路径错乱。
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="$ROOT_DIR/backend"
FRONTEND_DIR="$ROOT_DIR/frontend"
QDRANT_URL="http://127.0.0.1:6333/collections"
APP_AUTH_TOKEN_VALUE="${APP_AUTH_TOKEN:-123}"
BACKEND_PORT="8080"
FRONTEND_PORT="3000"

BACKEND_PID=""
FRONTEND_PID=""

cleanup() {
  local exit_code=$?

  # 收到 Ctrl+C 或任一子进程退出后，统一清理两个前台服务。
  if [ -n "${BACKEND_PID:-}" ] && kill -0 "$BACKEND_PID" >/dev/null 2>&1; then
    kill "$BACKEND_PID" >/dev/null 2>&1 || true
  fi
  if [ -n "${FRONTEND_PID:-}" ] && kill -0 "$FRONTEND_PID" >/dev/null 2>&1; then
    kill "$FRONTEND_PID" >/dev/null 2>&1 || true
  fi

  wait >/dev/null 2>&1 || true
  exit "$exit_code"
}

kill_port_listener() {
  local port="$1"
  local pids

  # 再次运行脚本时，先清理旧的本地开发监听进程，避免端口冲突。
  pids="$(lsof -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)"
  if [ -z "$pids" ]; then
    return 0
  fi

  echo "检测到端口 $port 已被占用，准备结束旧进程: $pids"
  kill $pids >/dev/null 2>&1 || true

  for _ in {1..10}; do
    if ! lsof -tiTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done

  pids="$(lsof -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)"
  if [ -n "$pids" ]; then
    echo "端口 $port 上的旧进程未正常退出，执行强制结束: $pids"
    kill -9 $pids >/dev/null 2>&1 || true
  fi

  if lsof -tiTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
    echo "错误：端口 $port 仍被占用，取消启动，避免开发服务误绑到其他端口。"
    return 1
  fi
}

if ! command -v go >/dev/null 2>&1; then
  echo "错误：未检测到 Go，请先安装 Go 1.21+。"
  exit 1
fi

if ! command -v npm >/dev/null 2>&1; then
  echo "错误：未检测到 npm，请先安装 Node.js 18+。"
  exit 1
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "错误：未检测到 curl，请先安装 curl。"
  exit 1
fi

if [ ! -d "$BACKEND_DIR" ]; then
  echo "错误：未找到 backend 目录：$BACKEND_DIR"
  exit 1
fi

if [ ! -d "$FRONTEND_DIR" ]; then
  echo "错误：未找到 frontend 目录：$FRONTEND_DIR"
  exit 1
fi

trap cleanup INT TERM EXIT

echo "前台启动 ai-localbase 开发环境..."
echo "后端地址： http://127.0.0.1:${BACKEND_PORT}"
echo "MCP 接口： http://127.0.0.1:${BACKEND_PORT}/mcp"
echo "前端地址： http://127.0.0.1:${FRONTEND_PORT}"
echo "应用访问令牌： ${APP_AUTH_TOKEN_VALUE}"
echo "按 Ctrl+C 可同时停止后端和前端。"

if ! curl -fsS "$QDRANT_URL" >/dev/null 2>&1; then
  echo "未检测到本地 Qdrant，准备执行 docker compose -f docker-compose.qdrant.yml up -d ..."

  if ! command -v docker >/dev/null 2>&1; then
    echo "错误：未检测到 docker，无法自动启动 Qdrant。"
    exit 1
  fi

  (
    cd "$ROOT_DIR"
    docker compose -f docker-compose.qdrant.yml up -d
  )

  echo "等待 Qdrant 就绪..."
  for _ in {1..20}; do
    if curl -fsS "$QDRANT_URL" >/dev/null 2>&1; then
      echo "Qdrant 已就绪。"
      break
    fi
    sleep 1
  done

  if ! curl -fsS "$QDRANT_URL" >/dev/null 2>&1; then
    echo "错误：Qdrant 启动后仍不可达，请检查 Docker 或端口 6333。"
    exit 1
  fi
else
  echo "检测到本地 Qdrant 已启动，跳过 Docker 启动。"
fi

kill_port_listener "$BACKEND_PORT"
kill_port_listener "$FRONTEND_PORT"

if [ ! -d "$FRONTEND_DIR/node_modules" ]; then
  echo "未检测到 frontend/node_modules，先执行 npm install..."
  (
    cd "$FRONTEND_DIR"
    npm install
  )
fi

(
  cd "$BACKEND_DIR"
  export APP_AUTH_TOKEN="$APP_AUTH_TOKEN_VALUE"
  exec go run .
) &
BACKEND_PID=$!

(
  cd "$FRONTEND_DIR"
  # 固定开发地址，避免端口冲突时 Vite 静默跳到 3001 仍误导用户访问 3000。
  exec npm run dev -- --host 127.0.0.1 --strictPort
) &
FRONTEND_PID=$!

# 任一子进程先退出时，结束整个开发会话并触发清理。
wait -n "$BACKEND_PID" "$FRONTEND_PID"
