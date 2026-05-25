#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
用法:
  ./scripts/rebuild-qdrant-index.sh <APP_AUTH_TOKEN>
  ./scripts/rebuild-qdrant-index.sh --token <APP_AUTH_TOKEN>

可选参数:
  --backend-url <url>     手动指定后端地址；默认自动探测本地 Docker 映射端口
  --kb-id <id>            默认 kb-0
  --kb-name <name>        默认 初始知识库
  --include-archives      同时导入 uploads/md 下的归档文件；默认不导入
  --help                  显示帮助

示例:
  ./scripts/rebuild-qdrant-index.sh change-me-app-token
  ./scripts/rebuild-qdrant-index.sh --token change-me-app-token --kb-name 初始知识库
EOF
}

detect_backend_url() {
  local published=""
  if command -v docker >/dev/null 2>&1; then
    published="$(docker compose port backend 8080 2>/dev/null | tail -n 1 || true)"
    if [[ -z "${published}" ]]; then
      published="$(docker port ai-localbase-backend 8080/tcp 2>/dev/null | tail -n 1 || true)"
    fi
  fi

  if [[ -n "${published}" ]]; then
    local port="${published##*:}"
    local host="${published%:*}"
    host="${host#[}"
    host="${host%]}"
    if [[ -z "${host}" || "${host}" == "0.0.0.0" || "${host}" == "::" ]]; then
      host="127.0.0.1"
    fi
    printf 'http://%s:%s\n' "${host}" "${port}"
    return
  fi

  printf 'http://127.0.0.1:%s\n' "${BACKEND_PORT:-${PORT:-8080}}"
}

BACKEND_URL="${BACKEND_URL:-}"
ACCESS_TOKEN=""
KB_ID="${KB_ID:-kb-0}"
KB_NAME="${KB_NAME:-初始知识库}"
INCLUDE_ARCHIVES="${INCLUDE_ARCHIVES:-false}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --token|-t)
      ACCESS_TOKEN="${2:-}"
      shift 2
      ;;
    --backend-url|-u)
      BACKEND_URL="${2:-}"
      shift 2
      ;;
    --kb-id)
      KB_ID="${2:-}"
      shift 2
      ;;
    --kb-name)
      KB_NAME="${2:-}"
      shift 2
      ;;
    --include-archives)
      INCLUDE_ARCHIVES="true"
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    --*)
      echo "未知参数: $1" >&2
      usage >&2
      exit 2
      ;;
    *)
      if [[ -z "${ACCESS_TOKEN}" ]]; then
        ACCESS_TOKEN="$1"
        shift
      else
        echo "多余参数: $1" >&2
        usage >&2
        exit 2
      fi
      ;;
  esac
done

if [[ -z "${BACKEND_URL}" ]]; then
  BACKEND_URL="$(detect_backend_url)"
fi

REQUEST_URL="${BACKEND_URL%/}/api/admin/rebuild-qdrant-index"
REQUEST_BODY=$(cat <<JSON
{
  "confirm": true,
  "knowledgeBaseId": "${KB_ID}",
  "knowledgeBaseName": "${KB_NAME}",
  "includeArchives": ${INCLUDE_ARCHIVES}
}
JSON
)

headers=(-H "Content-Type: application/json")
if [[ -n "${ACCESS_TOKEN}" ]]; then
  headers+=(-H "Authorization: Bearer ${ACCESS_TOKEN}")
fi

echo "== Qdrant 索引重建 ==" >&2
echo "后端接口: ${REQUEST_URL}" >&2
echo "目标知识库: ${KB_ID} (${KB_NAME})" >&2
echo "导入归档文件: ${INCLUDE_ARCHIVES}" >&2
if [[ -n "${ACCESS_TOKEN}" ]]; then
  echo "访问令牌: 已提供" >&2
else
  echo "访问令牌: 未提供" >&2
fi
echo "开始调用恢复接口..." >&2

response_file="$(mktemp)"
http_code="$(curl -sS -w '%{http_code}' -o "${response_file}" -X POST "${REQUEST_URL}" \
  "${headers[@]}" \
  --data-binary "${REQUEST_BODY}")"

echo "HTTP 状态码: ${http_code}" >&2
echo "响应内容:" >&2
cat "${response_file}"
echo
rm -f "${response_file}"

if [[ "${http_code}" -lt 200 || "${http_code}" -ge 300 ]]; then
  echo "恢复接口调用失败" >&2
  exit 1
fi

echo "恢复接口调用完成" >&2
