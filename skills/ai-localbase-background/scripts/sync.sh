#!/bin/bash

AI_LOCALBASE_BACKGROUND_SYNC_SKILL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
AI_LOCALBASE_BACKGROUND_SYNC_DEFAULT_ENV_FILE="$AI_LOCALBASE_BACKGROUND_SYNC_SKILL_DIR/.env"
AI_LOCALBASE_BACKGROUND_SYNC_STATE_DIR_NAME=".ai-localbase-background"

# 将任意文本安全转成 JSON 字符串内容，避免手拼请求体时被引号或换行破坏。
ai_localbase_background_sync_json_escape() {
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
ai_localbase_background_sync_extract_json_string_field() {
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

ai_localbase_background_sync_fail() {
  echo "错误: $1" >&2
  exit 1
}

ai_localbase_background_sync_ensure_requirements() {
  if ! command -v curl >/dev/null 2>&1; then
    ai_localbase_background_sync_fail "未找到 curl，请先安装 curl"
  fi
  if ! command -v python3 >/dev/null 2>&1; then
    ai_localbase_background_sync_fail "未找到 python3，请先安装 Python 3"
  fi
}

ai_localbase_background_sync_resolve_env_file() {
  if [ -f "$AI_LOCALBASE_BACKGROUND_SYNC_DEFAULT_ENV_FILE" ]; then
    printf '%s\n' "$AI_LOCALBASE_BACKGROUND_SYNC_DEFAULT_ENV_FILE"
    return 0
  fi

  return 1
}

ai_localbase_background_sync_load_env() {
  local env_file
  env_file="$(ai_localbase_background_sync_resolve_env_file)" || ai_localbase_background_sync_fail \
    "未找到 .env。请先在 ai-localbase-background skill 当前目录中配置 .env"

  set -a
  # shellcheck disable=SC1090
  source "$env_file"
  set +a

  : "${MCP_API_BASE_URL:?错误: $env_file 缺少 MCP_API_BASE_URL}"
  : "${MCP_AUTH_TOKEN:?错误: $env_file 缺少 MCP_AUTH_TOKEN}"

  export MCP_API_BASE_URL
  export MCP_AUTH_HEADER="Authorization: Bearer $MCP_AUTH_TOKEN"
  export AI_LOCALBASE_BACKGROUND_SYNC_ENV_FILE="$env_file"
}

ai_localbase_background_sync_resolve_work_dir() {
  local input="${1:-$(pwd)}"

  if [ -d "$input" ]; then
    (cd "$input" && pwd -P)
  else
    ai_localbase_background_sync_fail "目录不存在: $input"
  fi
}

ai_localbase_background_sync_is_project_root() {
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

ai_localbase_background_sync_resolve_project_work_dir() {
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

  if [ -n "$candidate" ] && ai_localbase_background_sync_is_project_root "$candidate"; then
    # docs 下任务目录只承载阶段文档；知识库和后台状态必须按项目启动目录归属。
    printf '%s\n' "$candidate"
    return 0
  fi

  printf '%s\n' "$dir"
}

ai_localbase_background_sync_resolve_kb_name() {
  local dir="$1"
  basename "$dir"
}

ai_localbase_background_sync_runtime_root() {
  local work_dir="$1"
  printf '%s/docs/%s\n' "$work_dir" "$AI_LOCALBASE_BACKGROUND_SYNC_STATE_DIR_NAME"
}

ai_localbase_background_sync_ensure_runtime_dirs() {
  mkdir -p "$AI_LOCALBASE_BACKGROUND_SYNC_STATE_DIR"
  mkdir -p "$AI_LOCALBASE_BACKGROUND_SYNC_STATE_DIR/queue"
  mkdir -p "$AI_LOCALBASE_BACKGROUND_SYNC_STATE_DIR/jobs"
  mkdir -p "$AI_LOCALBASE_BACKGROUND_SYNC_STATE_DIR/results"

  if [ ! -f "$AI_LOCALBASE_BACKGROUND_SYNC_KB_CONFIG" ]; then
    printf '{}\n' > "$AI_LOCALBASE_BACKGROUND_SYNC_KB_CONFIG"
  fi
  ai_localbase_background_sync_normalize_kb_config
}

ai_localbase_background_sync_normalize_kb_config() {
  # 旧版同步 fallback 使用字符串拼接写 JSON；这里先规范化并尽量恢复已有 kb-* 映射。
  python3 - "$AI_LOCALBASE_BACKGROUND_SYNC_KB_CONFIG" <<'PY'
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

ai_localbase_background_sync_read_cached_kb_id() {
  local key="$1"

  python3 - "$AI_LOCALBASE_BACKGROUND_SYNC_KB_CONFIG" "$key" <<'PY'
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

ai_localbase_background_sync_write_cached_kb_id() {
  local key="$1"
  local id="$2"

  python3 - "$AI_LOCALBASE_BACKGROUND_SYNC_KB_CONFIG" "$key" "$id" <<'PY'
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

ai_localbase_background_sync_post_tool_call() {
  local tool_name="$1"
  local body="$2"

  curl -s "$MCP_API_BASE_URL/tools/$tool_name/call" \
    -H 'Content-Type: application/json' \
    -H "$MCP_AUTH_HEADER" \
    -d "$body"
}

ai_localbase_background_sync_post_tools_list() {
  curl -s "$MCP_API_BASE_URL" \
    -H 'Content-Type: application/json' \
    -H "$MCP_AUTH_HEADER" \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
}

ai_localbase_background_sync_find_kb_id_by_name() {
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

ai_localbase_background_sync_prepare_context() {
  local work_dir_input="${1:-$(pwd)}"

  ai_localbase_background_sync_ensure_requirements
  ai_localbase_background_sync_load_env

  export AI_LOCALBASE_BACKGROUND_SYNC_WORK_DIR
  AI_LOCALBASE_BACKGROUND_SYNC_WORK_DIR="$(
    ai_localbase_background_sync_resolve_project_work_dir \
      "$(ai_localbase_background_sync_resolve_work_dir "$work_dir_input")"
  )"

  export AI_LOCALBASE_BACKGROUND_SYNC_KB_NAME
  AI_LOCALBASE_BACKGROUND_SYNC_KB_NAME="$(ai_localbase_background_sync_resolve_kb_name "$AI_LOCALBASE_BACKGROUND_SYNC_WORK_DIR")"

  export AI_LOCALBASE_BACKGROUND_SYNC_STATE_DIR
  AI_LOCALBASE_BACKGROUND_SYNC_STATE_DIR="$(ai_localbase_background_sync_runtime_root "$AI_LOCALBASE_BACKGROUND_SYNC_WORK_DIR")"

  export AI_LOCALBASE_BACKGROUND_SYNC_KB_CONFIG
  AI_LOCALBASE_BACKGROUND_SYNC_KB_CONFIG="$AI_LOCALBASE_BACKGROUND_SYNC_STATE_DIR/knowledge.json"

  ai_localbase_background_sync_ensure_runtime_dirs
}

ai_localbase_background_sync_ensure_kb_id() {
  local tools_response
  local list_response

  tools_response="$(ai_localbase_background_sync_post_tools_list)"
  if [ -z "$tools_response" ]; then
    ai_localbase_background_sync_fail "读取 tools/list 失败: 响应为空"
  fi

  list_response="$(ai_localbase_background_sync_post_tool_call "knowledge_base.list" '{"arguments":{}}')"
  if [ -z "$list_response" ]; then
    ai_localbase_background_sync_fail "读取 knowledge_base.list 失败: 响应为空"
  fi

  AI_LOCALBASE_BACKGROUND_SYNC_KB_ID="$(
    ai_localbase_background_sync_find_kb_id_by_name \
      "$list_response" \
      "$AI_LOCALBASE_BACKGROUND_SYNC_KB_NAME" || true
  )"
  if [ -n "${AI_LOCALBASE_BACKGROUND_SYNC_KB_ID:-}" ]; then
    ai_localbase_background_sync_write_cached_kb_id \
      "$AI_LOCALBASE_BACKGROUND_SYNC_KB_NAME" \
      "$AI_LOCALBASE_BACKGROUND_SYNC_KB_ID"
    export AI_LOCALBASE_BACKGROUND_SYNC_KB_ID
    return 0
  fi

  local response
  response="$(ai_localbase_background_sync_post_tool_call "knowledge_base.create" "$(printf '{"arguments":{"name":"%s","description":"%s"}}' \
    "$(ai_localbase_background_sync_json_escape "$AI_LOCALBASE_BACKGROUND_SYNC_KB_NAME")" \
    "$(ai_localbase_background_sync_json_escape "目录: $AI_LOCALBASE_BACKGROUND_SYNC_WORK_DIR")")")"

  AI_LOCALBASE_BACKGROUND_SYNC_KB_ID="$(
    ai_localbase_background_sync_extract_json_string_field "$response" "knowledgeBaseId" || true
  )"
  if [ -z "${AI_LOCALBASE_BACKGROUND_SYNC_KB_ID:-}" ]; then
    ai_localbase_background_sync_fail "创建知识库失败: $response"
  fi

  ai_localbase_background_sync_write_cached_kb_id \
    "$AI_LOCALBASE_BACKGROUND_SYNC_KB_NAME" \
    "$AI_LOCALBASE_BACKGROUND_SYNC_KB_ID"

  export AI_LOCALBASE_BACKGROUND_SYNC_KB_ID
}

ai_localbase_background_sync_tools() {
  ai_localbase_background_sync_ensure_requirements
  ai_localbase_background_sync_load_env
  ai_localbase_background_sync_post_tools_list
}

ai_localbase_background_sync_list() {
  ai_localbase_background_sync_ensure_requirements
  ai_localbase_background_sync_load_env
  ai_localbase_background_sync_post_tool_call "knowledge_base.list" '{"arguments":{}}'
}

ai_localbase_background_sync_init() {
  local work_dir_input="${1:-$(pwd)}"
  ai_localbase_background_sync_prepare_context "$work_dir_input"
  ai_localbase_background_sync_ensure_kb_id

  printf '{"status":"ok","mode":"sync","workDir":"%s","knowledgeBaseName":"%s","knowledgeBaseId":"%s","stateDir":"%s","envFile":"%s"}\n' \
    "$(ai_localbase_background_sync_json_escape "$AI_LOCALBASE_BACKGROUND_SYNC_WORK_DIR")" \
    "$(ai_localbase_background_sync_json_escape "$AI_LOCALBASE_BACKGROUND_SYNC_KB_NAME")" \
    "$(ai_localbase_background_sync_json_escape "$AI_LOCALBASE_BACKGROUND_SYNC_KB_ID")" \
    "$(ai_localbase_background_sync_json_escape "$AI_LOCALBASE_BACKGROUND_SYNC_STATE_DIR")" \
    "$(ai_localbase_background_sync_json_escape "$AI_LOCALBASE_BACKGROUND_SYNC_ENV_FILE")"
}

ai_localbase_background_sync_upload() {
  local filename="${1:-example.md}"
  local content="${2:-# 示例文档

这是测试内容。}"
  local work_dir_input="${3:-$(pwd)}"
  local response

  ai_localbase_background_sync_prepare_context "$work_dir_input"
  ai_localbase_background_sync_ensure_kb_id

  response="$(ai_localbase_background_sync_post_tool_call "document.upload" "$(printf '{"arguments":{"knowledgeBaseId":"%s","filename":"%s","content":"%s"}}' \
    "$(ai_localbase_background_sync_json_escape "$AI_LOCALBASE_BACKGROUND_SYNC_KB_ID")" \
    "$(ai_localbase_background_sync_json_escape "$filename")" \
    "$(ai_localbase_background_sync_json_escape "$content")")")"
  printf '%s\n' "$response"
}

ai_localbase_background_sync_append() {
  local document_id="${1:-}"
  local content="${2:-}"
  local work_dir_input="${3:-$(pwd)}"
  local response

  if [ -z "$document_id" ] || [ -z "$content" ]; then
    ai_localbase_background_sync_fail "append 需要 [documentId] [内容] [目录]"
  fi

  ai_localbase_background_sync_prepare_context "$work_dir_input"
  ai_localbase_background_sync_ensure_kb_id

  response="$(ai_localbase_background_sync_post_tool_call "document.append" "$(printf '{"arguments":{"knowledgeBaseId":"%s","documentId":"%s","content":"%s"}}' \
    "$(ai_localbase_background_sync_json_escape "$AI_LOCALBASE_BACKGROUND_SYNC_KB_ID")" \
    "$(ai_localbase_background_sync_json_escape "$document_id")" \
    "$(ai_localbase_background_sync_json_escape "$content")")")"
  printf '%s\n' "$response"
}

ai_localbase_background_sync_update() {
  local document_id="${1:-}"
  local content="${2:-}"
  local work_dir_input="${3:-$(pwd)}"
  local response

  if [ -z "$document_id" ] || [ -z "$content" ]; then
    ai_localbase_background_sync_fail "update 需要 [documentId] [内容] [目录]"
  fi

  ai_localbase_background_sync_prepare_context "$work_dir_input"
  ai_localbase_background_sync_ensure_kb_id

  response="$(ai_localbase_background_sync_post_tool_call "document.update" "$(printf '{"arguments":{"knowledgeBaseId":"%s","documentId":"%s","content":"%s"}}' \
    "$(ai_localbase_background_sync_json_escape "$AI_LOCALBASE_BACKGROUND_SYNC_KB_ID")" \
    "$(ai_localbase_background_sync_json_escape "$document_id")" \
    "$(ai_localbase_background_sync_json_escape "$content")")")"
  printf '%s\n' "$response"
}

ai_localbase_background_sync_delete() {
  local document_id="${1:-}"
  local work_dir_input="${2:-$(pwd)}"
  local response

  if [ -z "$document_id" ]; then
    ai_localbase_background_sync_fail "delete 需要 [documentId] [目录]"
  fi

  ai_localbase_background_sync_prepare_context "$work_dir_input"
  ai_localbase_background_sync_ensure_kb_id

  response="$(ai_localbase_background_sync_post_tool_call "document.delete" "$(printf '{"arguments":{"knowledgeBaseId":"%s","documentId":"%s"}}' \
    "$(ai_localbase_background_sync_json_escape "$AI_LOCALBASE_BACKGROUND_SYNC_KB_ID")" \
    "$(ai_localbase_background_sync_json_escape "$document_id")")")"
  printf '%s\n' "$response"
}

ai_localbase_background_sync_parse_query_args() {
  if [ -d "${1:-}" ]; then
    AI_LOCALBASE_BACKGROUND_SYNC_WORK_DIR_ARG="$1"
    AI_LOCALBASE_BACKGROUND_SYNC_QUERY_ARG="${2:-示例}"
    AI_LOCALBASE_BACKGROUND_SYNC_TOP_K_ARG="${3:-3}"
  else
    AI_LOCALBASE_BACKGROUND_SYNC_QUERY_ARG="${1:-示例}"
    AI_LOCALBASE_BACKGROUND_SYNC_WORK_DIR_ARG="${2:-$(pwd)}"
    AI_LOCALBASE_BACKGROUND_SYNC_TOP_K_ARG="${3:-3}"
  fi

  if ! [[ "$AI_LOCALBASE_BACKGROUND_SYNC_TOP_K_ARG" =~ ^[0-9]+$ ]]; then
    ai_localbase_background_sync_fail "search 的 topK 必须是数字: $AI_LOCALBASE_BACKGROUND_SYNC_TOP_K_ARG"
  fi
}

ai_localbase_background_sync_parse_message_args() {
  if [ -d "${1:-}" ]; then
    AI_LOCALBASE_BACKGROUND_SYNC_WORK_DIR_ARG="$1"
    AI_LOCALBASE_BACKGROUND_SYNC_MESSAGE_ARG="${2:-这是什么内容？}"
  else
    AI_LOCALBASE_BACKGROUND_SYNC_MESSAGE_ARG="${1:-这是什么内容？}"
    AI_LOCALBASE_BACKGROUND_SYNC_WORK_DIR_ARG="${2:-$(pwd)}"
  fi
}

ai_localbase_background_sync_search() {
  local response

  ai_localbase_background_sync_parse_query_args "$@"
  ai_localbase_background_sync_prepare_context "$AI_LOCALBASE_BACKGROUND_SYNC_WORK_DIR_ARG"
  ai_localbase_background_sync_ensure_kb_id

  response="$(ai_localbase_background_sync_post_tool_call "knowledge_base.search" "$(printf '{"arguments":{"knowledgeBaseId":"%s","query":"%s","topK":%s}}' \
    "$(ai_localbase_background_sync_json_escape "$AI_LOCALBASE_BACKGROUND_SYNC_KB_ID")" \
    "$(ai_localbase_background_sync_json_escape "$AI_LOCALBASE_BACKGROUND_SYNC_QUERY_ARG")" \
    "$AI_LOCALBASE_BACKGROUND_SYNC_TOP_K_ARG")")"
  printf '%s\n' "$response"
}

ai_localbase_background_sync_chat() {
  local response

  ai_localbase_background_sync_parse_message_args "$@"
  ai_localbase_background_sync_prepare_context "$AI_LOCALBASE_BACKGROUND_SYNC_WORK_DIR_ARG"
  ai_localbase_background_sync_ensure_kb_id

  response="$(ai_localbase_background_sync_post_tool_call "chat.ask" "$(printf '{"arguments":{"knowledgeBaseId":"%s","message":"%s"}}' \
    "$(ai_localbase_background_sync_json_escape "$AI_LOCALBASE_BACKGROUND_SYNC_KB_ID")" \
    "$(ai_localbase_background_sync_json_escape "$AI_LOCALBASE_BACKGROUND_SYNC_MESSAGE_ARG")")")"
  printf '%s\n' "$response"
}
