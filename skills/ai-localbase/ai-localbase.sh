#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$SCRIPT_DIR/.env"
KB_CONFIG="$SCRIPT_DIR/knowledge.json"

usage() {
  cat <<'EOF'
用法:
  ./ai-localbase.sh init [目录]
  ./ai-localbase.sh tools
  ./ai-localbase.sh list
  ./ai-localbase.sh upload [文件名] [内容] [目录]
  ./ai-localbase.sh append [documentId] [内容] [目录]
  ./ai-localbase.sh update [documentId] [内容] [目录]
  ./ai-localbase.sh delete [documentId] [目录]
  ./ai-localbase.sh search [关键词] [目录] [topK]
  ./ai-localbase.sh chat [问题] [目录]

说明:
  - init: 初始化当前目录对应的知识库映射并输出摘要 JSON
  - tools: 通过 tools/list 列出当前可用工具能力、调用方式、参数和响应字段
  - list: 通过 knowledge_base.list 列出现有知识库名称和知识库 ID
  - upload: 上传文本内容到知识库
  - append: 向已有文档追加文本内容
  - update: 用新内容覆盖已有文档
  - delete: 删除已有文档
  - search: 在知识库中检索片段
  - chat: 基于知识库上下文发起问答
EOF
}

# 将任意文本安全转成 JSON 字符串内容，避免手拼请求体时被引号或换行破坏。
json_escape() {
  local value="${1-}"
  value=${value//\\/\\\\}
  value=${value//\"/\\\"}
  value=${value//$'\n'/\\n}
  value=${value//$'\r'/\\r}
  value=${value//$'\t'/\\t}
  value=${value//$'\f'/\\f}
  value=${value//$'\b'/\\b}
  printf '%s' "$value"
}

# 从 JSON 响应里提取目标字符串字段，兼容 structuredContent 等嵌套结构。
extract_json_string_field() {
  local json="$1"
  local field="$2"

  JSON_INPUT="$json" FIELD="$field" python3 - <<'PY'
import json
import os
import sys

field = os.environ["FIELD"]
raw = os.environ["JSON_INPUT"]

def find_value(value):
    if isinstance(value, dict):
        if isinstance(value.get(field), str):
            return value[field]
        for item in value.values():
            found = find_value(item)
            if found:
                return found
    elif isinstance(value, list):
        for item in value:
            found = find_value(item)
            if found:
                return found
    return None

try:
    data = json.loads(raw)
except Exception:
    sys.exit(1)

result = find_value(data)
if not result:
    sys.exit(1)
print(result)
PY
}

ensure_requirements() {
  if ! command -v curl >/dev/null 2>&1; then
    echo "错误: 未找到 curl，请先安装 curl"
    exit 1
  fi
  if ! command -v python3 >/dev/null 2>&1; then
    echo "错误: 未找到 python3，请先安装 Python 3"
    exit 1
  fi
}

load_env() {
  if [ ! -f "$ENV_FILE" ]; then
    echo "错误: .env 文件不存在，请复制 .env.example 并配置"
    exit 1
  fi

  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a

  : "${MCP_API_BASE_URL:?错误: .env 中缺少 MCP_API_BASE_URL}"
  : "${MCP_AUTH_TOKEN:?错误: .env 中缺少 MCP_AUTH_TOKEN}"

  export MCP_API_BASE_URL
  export MCP_AUTH_HEADER="Authorization: Bearer $MCP_AUTH_TOKEN"
}

resolve_work_dir() {
  local input="${1:-$(pwd)}"

  if [ -d "$input" ]; then
    (cd "$input" && pwd -P)
  else
    printf '%s\n' "$input"
  fi
}

is_project_root() {
  local path="$1"

  [ -d "$path" ] || return 1

  [ -f "$path/AGENTS.md" ] ||
    [ -f "$path/agents.md" ] ||
    [ -d "$path/.git" ] ||
    [ -f "$path/package.json" ] ||
    [ -f "$path/composer.json" ] ||
    [ -f "$path/go.mod" ] ||
    [ -f "$path/pyproject.toml" ] ||
    [ -f "$path/README.md" ]
}

resolve_project_work_dir() {
  local dir="$1"
  local normalized="${dir//\\//}"
  local candidate=""

  case "$normalized" in
    */docs/*)
      candidate="${normalized%%/docs/*}"
      ;;
    */docs)
      candidate="${normalized%/docs}"
      ;;
  esac

  if [ -n "$candidate" ] && is_project_root "$candidate"; then
    # docs 下任务目录只承载阶段文档；知识库必须按项目启动目录归属。
    printf '%s\n' "$candidate"
    return 0
  fi

  printf '%s\n' "$dir"
}

resolve_kb_name() {
  local dir="$1"
  basename "$dir"
}

ensure_kb_config() {
  if [ ! -f "$KB_CONFIG" ]; then
    printf '{}\n' > "$KB_CONFIG"
  fi
  normalize_kb_config
}

normalize_kb_config() {
  # 读取缓存时先规范化；若旧版纯 shell 拼接导致 JSON 损坏，则尽量恢复已有 kb-* 映射。
  python3 - "$KB_CONFIG" <<'PY'
import json
import os
import re
import sys

path = sys.argv[1]

try:
    with open(path, "r", encoding="utf-8-sig") as fh:
        raw = fh.read()
except FileNotFoundError:
    raw = ""

def recover_pairs(text):
    pairs = {}
    pattern = r'"((?:[^"\\]|\\.)+)"\s*:\s*"((?:[^"\\]|\\.)+)"'
    for raw_key, raw_value in re.findall(pattern, text):
        try:
            key = json.loads(f'"{raw_key}"')
            value = json.loads(f'"{raw_value}"')
        except Exception:
            continue
        if isinstance(value, str) and value.startswith("kb-"):
            pairs[str(key)] = value
    return pairs

try:
    data = json.loads(raw) if raw.strip() else {}
    if not isinstance(data, dict):
        data = {}
    data = {
        str(key): str(value)
        for key, value in data.items()
        if isinstance(value, str) and value
    }
except Exception:
    data = recover_pairs(raw)

tmp_path = f"{path}.tmp"
with open(tmp_path, "w", encoding="utf-8") as fh:
    json.dump(data, fh, ensure_ascii=False, indent=2, sort_keys=True)
    fh.write("\n")
os.replace(tmp_path, path)
PY
}

read_cached_kb_id() {
  local dir="$1"

  python3 - "$KB_CONFIG" "$dir" <<'PY'
import json
import sys

path = sys.argv[1]
key = sys.argv[2]

with open(path, "r", encoding="utf-8") as fh:
    data = json.load(fh)

value = data.get(key)
if not isinstance(value, str) or not value:
    sys.exit(1)
print(value)
PY
}

write_cached_kb_id() {
  local dir="$1"
  local id="$2"

  python3 - "$KB_CONFIG" "$dir" "$id" <<'PY'
import json
import os
import sys

path = sys.argv[1]
key = sys.argv[2]
value = sys.argv[3]

with open(path, "r", encoding="utf-8") as fh:
    data = json.load(fh)
if not isinstance(data, dict):
    data = {}

data[key] = value
tmp_path = f"{path}.tmp"
with open(tmp_path, "w", encoding="utf-8") as fh:
    json.dump(data, fh, ensure_ascii=False, indent=2, sort_keys=True)
    fh.write("\n")
os.replace(tmp_path, path)
PY
}

post_tool_call() {
  local tool_name="$1"
  local body="$2"

  curl -s "$MCP_API_BASE_URL/tools/$tool_name/call" \
    -H 'Content-Type: application/json' \
    -H "$MCP_AUTH_HEADER" \
    -d "$body"
}

post_tools_list() {
  curl -s "$MCP_API_BASE_URL" \
    -H 'Content-Type: application/json' \
    -H "$MCP_AUTH_HEADER" \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
}

find_kb_id_by_name() {
  local json="$1"
  local kb_name="$2"

  JSON_INPUT="$json" KB_NAME_INPUT="$kb_name" python3 - <<'PY'
import json
import os
import sys

target = os.environ["KB_NAME_INPUT"]
raw = os.environ["JSON_INPUT"]

try:
    data = json.loads(raw)
except Exception:
    sys.exit(1)

items = data.get("structuredContent", {}).get("items", [])
if not isinstance(items, list):
    sys.exit(1)

for item in items:
    if not isinstance(item, dict):
        continue
    if item.get("name") != target:
        continue
    kb_id = item.get("knowledgeBaseId") or item.get("id")
    if isinstance(kb_id, str) and kb_id:
        print(kb_id)
        sys.exit(0)

sys.exit(1)
PY
}

prepare_context() {
  local work_dir_input="${1:-$(pwd)}"

  ensure_requirements
  load_env
  ensure_kb_config

  export WORK_DIR
  WORK_DIR="$(resolve_project_work_dir "$(resolve_work_dir "$work_dir_input")")"
  export KB_NAME
  KB_NAME="$(resolve_kb_name "$WORK_DIR")"
}

ensure_kb_id() {
  local tools_response
  local list_response

  echo "正在读取工具能力列表..."
  tools_response="$(post_tools_list)"
  if [ -z "$tools_response" ]; then
    echo "读取 tools/list 失败: 响应为空"
    exit 1
  fi

  echo "正在检索已有知识库..."
  list_response="$(post_tool_call "knowledge_base.list" '{"arguments":{}}')"
  if [ -z "$list_response" ]; then
    echo "读取 knowledge_base.list 失败: 响应为空"
    exit 1
  fi

  KB_ID="$(find_kb_id_by_name "$list_response" "$KB_NAME" || true)"
  if [ -n "${KB_ID:-}" ]; then
    write_cached_kb_id "$KB_NAME" "$KB_ID"
    echo "匹配到已有知识库: $KB_NAME (ID: $KB_ID)"
    export KB_ID
    return 0
  fi

  echo "未匹配到知识库名 $KB_NAME，正在创建..."

  local response
  response="$(post_tool_call "knowledge_base.create" "$(printf '{"arguments":{"name":"%s","description":"%s"}}' \
    "$(json_escape "$KB_NAME")" "$(json_escape "目录: $WORK_DIR")")")"

  KB_ID="$(extract_json_string_field "$response" "knowledgeBaseId" || true)"
  if [ -z "${KB_ID:-}" ]; then
    echo "创建知识库失败: $response"
    exit 1
  fi

  write_cached_kb_id "$KB_NAME" "$KB_ID"
  echo "知识库创建成功: $KB_NAME (ID: $KB_ID)"
  export KB_ID
}

cmd_tools() {
  ensure_requirements
  load_env
  post_tools_list
}

cmd_list() {
  ensure_requirements
  load_env
  post_tool_call "knowledge_base.list" '{"arguments":{}}'
}

cmd_init() {
  local work_dir_input="${1:-$(pwd)}"
  prepare_context "$work_dir_input"
  ensure_kb_id
  printf '{"workDir":"%s","knowledgeBaseName":"%s","knowledgeBaseId":"%s"}\n' \
    "$(json_escape "$WORK_DIR")" "$(json_escape "$KB_NAME")" "$(json_escape "$KB_ID")"
}

cmd_upload() {
  local filename="${1:-example.md}"
  local content="${2:-# 示例文档

这是测试内容。}"
  local work_dir_input="${3:-$(pwd)}"
  local response

  prepare_context "$work_dir_input"
  ensure_kb_id

  echo "上传文档到知识库: $KB_ID"
  response="$(post_tool_call "document.upload" "$(printf '{"arguments":{"knowledgeBaseId":"%s","filename":"%s","content":"%s"}}' \
    "$(json_escape "$KB_ID")" "$(json_escape "$filename")" "$(json_escape "$content")")")"
  printf '%s\n' "$response"
}

cmd_append() {
  local document_id="${1:-}"
  local content="${2:-}"
  local work_dir_input="${3:-$(pwd)}"
  local response

  if [ -z "$document_id" ] || [ -z "$content" ]; then
    echo "错误: append 需要 [documentId] [内容] [目录]"
    usage
    exit 1
  fi

  prepare_context "$work_dir_input"
  ensure_kb_id

  echo "追加文档到知识库: $KB_ID (文档: $document_id)"
  response="$(post_tool_call "document.append" "$(printf '{"arguments":{"knowledgeBaseId":"%s","documentId":"%s","content":"%s"}}' \
    "$(json_escape "$KB_ID")" "$(json_escape "$document_id")" "$(json_escape "$content")")")"
  printf '%s\n' "$response"
}

cmd_update() {
  local document_id="${1:-}"
  local content="${2:-}"
  local work_dir_input="${3:-$(pwd)}"
  local response

  if [ -z "$document_id" ] || [ -z "$content" ]; then
    echo "错误: update 需要 [documentId] [内容] [目录]"
    usage
    exit 1
  fi

  prepare_context "$work_dir_input"
  ensure_kb_id

  echo "覆盖文档到知识库: $KB_ID (文档: $document_id)"
  response="$(post_tool_call "document.update" "$(printf '{"arguments":{"knowledgeBaseId":"%s","documentId":"%s","content":"%s"}}' \
    "$(json_escape "$KB_ID")" "$(json_escape "$document_id")" "$(json_escape "$content")")")"
  printf '%s\n' "$response"
}

cmd_delete() {
  local document_id="${1:-}"
  local work_dir_input="${2:-$(pwd)}"
  local response

  if [ -z "$document_id" ]; then
    echo "错误: delete 需要 [documentId] [目录]"
    usage
    exit 1
  fi

  prepare_context "$work_dir_input"
  ensure_kb_id

  echo "删除文档: $KB_ID (文档: $document_id)"
  response="$(post_tool_call "document.delete" "$(printf '{"arguments":{"knowledgeBaseId":"%s","documentId":"%s"}}' \
    "$(json_escape "$KB_ID")" "$(json_escape "$document_id")")")"
  printf '%s\n' "$response"
}

cmd_search() {
  local query="${1:-示例}"
  local work_dir_input="${2:-$(pwd)}"
  local top_k="${3:-3}"
  local response

  if ! [[ "$top_k" =~ ^[0-9]+$ ]]; then
    echo "错误: topK 必须是非负整数"
    exit 1
  fi

  prepare_context "$work_dir_input"
  ensure_kb_id

  echo "检索: $query (知识库: $KB_ID)"
  response="$(post_tool_call "knowledge_base.search" "$(printf '{"arguments":{"knowledgeBaseId":"%s","query":"%s","topK":%s}}' \
    "$(json_escape "$KB_ID")" "$(json_escape "$query")" "$(json_escape "$top_k")")")"
  printf '%s\n' "$response"
}

cmd_chat() {
  local message="${1:-这是什么内容？}"
  local work_dir_input="${2:-$(pwd)}"
  local response

  prepare_context "$work_dir_input"
  ensure_kb_id

  echo "问答: $message (知识库: $KB_ID)"
  response="$(post_tool_call "chat.ask" "$(printf '{"arguments":{"knowledgeBaseId":"%s","message":"%s"}}' \
    "$(json_escape "$KB_ID")" "$(json_escape "$message")")")"
  printf '%s\n' "$response"
}

main() {
  local action="${1:-help}"
  shift || true

  case "$action" in
    init)
      cmd_init "${1:-$(pwd)}"
      ;;
    tools)
      cmd_tools
      ;;
    list)
      cmd_list
      ;;
    upload)
      cmd_upload "${1:-example.md}" "${2:-# 示例文档

这是测试内容。}" "${3:-$(pwd)}"
      ;;
    append)
      cmd_append "${1:-}" "${2:-}" "${3:-$(pwd)}"
      ;;
    update)
      cmd_update "${1:-}" "${2:-}" "${3:-$(pwd)}"
      ;;
    delete)
      cmd_delete "${1:-}" "${2:-$(pwd)}"
      ;;
    search)
      cmd_search "${1:-示例}" "${2:-$(pwd)}" "${3:-3}"
      ;;
    chat)
      cmd_chat "${1:-这是什么内容？}" "${2:-$(pwd)}"
      ;;
    help|-h|--help)
      usage
      ;;
    *)
      echo "错误: 不支持的动作 $action"
      usage
      exit 1
      ;;
  esac
}

main "$@"
