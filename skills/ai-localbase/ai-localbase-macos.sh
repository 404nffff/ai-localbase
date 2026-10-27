#!/bin/bash
set -euo pipefail

# 按入口所在目录引用公共命令，兼容安装目录软链接及包含空格的路径。
AI_LOCALBASE_MACOS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=ai-localbase.sh
source "$AI_LOCALBASE_MACOS_DIR/ai-localbase.sh"

# 加载同目录配置并导出认证环境；缺失或为空时停止，不发送网络请求。
# macOS Bash 3.2 的 source <(...) 可能静默漏读，改用标准输入加载 CRLF 归一后的正文。
load_env() {
  if [ ! -f "$ENV_FILE" ]; then
    echo "错误: .env 文件不存在，请复制 .env.example 并配置"
    exit 1
  fi

  local env_content
  env_content="$(sed $'s/\r$//' "$ENV_FILE")"
  set -a
  # shellcheck disable=SC1091
  source /dev/stdin <<< "$env_content"
  set +a

  : "${MCP_API_BASE_URL:?错误: .env 中缺少 MCP_API_BASE_URL}"
  : "${MCP_AUTH_TOKEN:?错误: .env 中缺少 MCP_AUTH_TOKEN}"

  export MCP_API_BASE_URL
  export MCP_AUTH_HEADER="Authorization: Bearer $MCP_AUTH_TOKEN"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
