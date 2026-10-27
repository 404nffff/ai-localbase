#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKER_SCRIPT="$SCRIPT_DIR/scripts/worker.py"
SYNC_HELPER_SCRIPT="$SCRIPT_DIR/scripts/sync.sh"

# shellcheck disable=SC1090
source "$SYNC_HELPER_SCRIPT"

usage() {
  cat <<'EOF'
用法:
  ./ai-localbase-background.sh init [目录]
  ./ai-localbase-background.sh tools
  ./ai-localbase-background.sh list
  ./ai-localbase-background.sh upload [文件名] [内容] [目录]
  ./ai-localbase-background.sh append [documentId] [内容] [目录]
  ./ai-localbase-background.sh update [documentId] [内容] [目录]
  ./ai-localbase-background.sh delete [documentId] [目录]
  ./ai-localbase-background.sh worker-start [目录]
  ./ai-localbase-background.sh worker-status [目录]
  ./ai-localbase-background.sh worker-logs [目录] [行数]
  ./ai-localbase-background.sh worker-stop [目录]
  ./ai-localbase-background.sh queue-upload [文件名] [内容] [目录]
  ./ai-localbase-background.sh queue-append [documentId] [内容] [目录]
  ./ai-localbase-background.sh queue-update [documentId] [内容] [目录]
  ./ai-localbase-background.sh queue-delete [documentId] [目录]
  ./ai-localbase-background.sh search [关键词] [目录] [topK]
  ./ai-localbase-background.sh search [目录] [关键词] [topK]
  ./ai-localbase-background.sh chat [问题] [目录]
  ./ai-localbase-background.sh chat [目录] [问题]
  ./ai-localbase-background.sh job-status [jobId] [目录]
  ./ai-localbase-background.sh job-result [jobId] [目录]

说明:
  - init: 初始化当前项目对应的知识库映射与运行状态目录
  - tools: 通过 tools/list 列出当前可用工具能力、调用方式、参数和响应字段
  - list: 通过 knowledge_base.list 列出现有知识库名称和知识库 ID
  - upload: 立即同步上传文本内容
  - append: 立即同步追加文档内容
  - update: 立即同步覆盖文档内容
  - delete: 立即同步删除文档
  - worker-start: 启动当前项目的后台 worker
  - worker-status: 查看当前项目后台 worker 状态
  - worker-logs: 输出当前项目 worker 日志尾部
  - worker-stop: 停止当前项目后台 worker
  - queue-upload: 将上传任务写入后台队列
  - queue-append: 将追加文档任务写入后台队列
  - queue-update: 将覆盖文档任务写入后台队列
  - queue-delete: 将删除文档任务写入后台队列
  - search: 同步检索
  - chat: 同步问答
  - job-status: 查看指定任务状态
  - job-result: 查看指定任务结果
EOF
}

find_python() {
  local candidate

  for candidate in python3 python; do
    if ! command -v "$candidate" >/dev/null 2>&1; then
      continue
    fi

    if "$candidate" -c 'import sys; raise SystemExit(0 if sys.version_info[0] >= 3 else 1)' >/dev/null 2>&1; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done

  return 1
}

python_hint_text() {
  printf '%s\n' "未检测到可用的 Python 3"
}

find_python_error_text() {
  printf '%s\n' "$(python_hint_text)，无法执行后台 worker 命令"
}

find_python_async_error_text() {
  local action="$1"
  printf '%s\n' "$(python_hint_text)，无法执行 $action。"
}

warn_sync_fallback() {
  local async_action="$1"
  echo "提示: $(python_hint_text)，$async_action 已回退为同步执行。" >&2
}

run_worker() {
  local python_bin
  python_bin="$(find_python)" || {
    echo "错误: $(find_python_error_text)" >&2
    return 1
  }

  "$python_bin" "$WORKER_SCRIPT" "$@"
}

has_python() {
  find_python >/dev/null 2>&1
}

require_python_for_async() {
  local action="$1"
  if has_python; then
    return 0
  fi

  echo "错误: $(find_python_async_error_text "$action")" >&2
  exit 1
}

main() {
  local action="${1:-help}"
  shift || true

  case "$action" in
    init)
      require_python_for_async "init"
      run_worker init --workdir "${1:-$(pwd)}"
      ;;
    tools)
      ai_localbase_background_sync_tools
      ;;
    list)
      ai_localbase_background_sync_list
      ;;
    upload)
      ai_localbase_background_sync_upload "${1:-example.md}" "${2:-# 示例文档

这是测试内容。}" "${3:-$(pwd)}"
      ;;
    append)
      ai_localbase_background_sync_append "${1:-}" "${2:-}" "${3:-$(pwd)}"
      ;;
    update)
      ai_localbase_background_sync_update "${1:-}" "${2:-}" "${3:-$(pwd)}"
      ;;
    delete)
      ai_localbase_background_sync_delete "${1:-}" "${2:-$(pwd)}"
      ;;
    worker-start)
      require_python_for_async "worker-start"
      run_worker worker-start --workdir "${1:-$(pwd)}"
      ;;
    worker-status)
      require_python_for_async "worker-status"
      run_worker worker-status --workdir "${1:-$(pwd)}"
      ;;
    worker-logs)
      require_python_for_async "worker-logs"
      run_worker worker-logs --workdir "${1:-$(pwd)}" --lines "${2:-50}"
      ;;
    worker-stop)
      require_python_for_async "worker-stop"
      run_worker worker-stop --workdir "${1:-$(pwd)}"
      ;;
    queue-upload)
      require_python_for_async "queue-upload"
      run_worker queue-upload --filename "${1:-}" --content "${2:-}" --workdir "${3:-$(pwd)}"
      ;;
    queue-append)
      require_python_for_async "queue-append"
      run_worker queue-append --document-id "${1:-}" --content "${2:-}" --workdir "${3:-$(pwd)}"
      ;;
    queue-update)
      require_python_for_async "queue-update"
      run_worker queue-update --document-id "${1:-}" --content "${2:-}" --workdir "${3:-$(pwd)}"
      ;;
    queue-delete)
      require_python_for_async "queue-delete"
      run_worker queue-delete --document-id "${1:-}" --workdir "${2:-$(pwd)}"
      ;;
    search)
      ai_localbase_background_sync_search "$@"
      ;;
    chat)
      ai_localbase_background_sync_chat "$@"
      ;;
    job-status)
      require_python_for_async "job-status"
      run_worker job-status --job-id "${1:-}" --workdir "${2:-$(pwd)}"
      ;;
    job-result)
      require_python_for_async "job-result"
      run_worker job-result --job-id "${1:-}" --workdir "${2:-$(pwd)}"
      ;;
    help|-h|--help)
      usage
      ;;
    *)
      echo "错误: 不支持的动作 $action" >&2
      usage
      exit 1
      ;;
  esac
}

main "$@"
