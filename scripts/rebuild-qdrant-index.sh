#!/usr/bin/env bash
set -euo pipefail

BACKEND_URL="${BACKEND_URL:-http://127.0.0.1:${PORT:-8080}}"
ACCESS_TOKEN="${ACCESS_TOKEN:-${APP_AUTH_TOKEN:-}}"
KB_ID="${KB_ID:-kb-0}"
KB_NAME="${KB_NAME:-初始知识库}"
INCLUDE_ARCHIVES="${INCLUDE_ARCHIVES:-false}"

headers=(-H "Content-Type: application/json")
if [[ -n "${ACCESS_TOKEN}" ]]; then
  headers+=(-H "Authorization: Bearer ${ACCESS_TOKEN}")
fi

curl -sS -X POST "${BACKEND_URL%/}/api/admin/rebuild-qdrant-index" \
  "${headers[@]}" \
  --data-binary @- <<JSON
{
  "confirm": true,
  "knowledgeBaseId": "${KB_ID}",
  "knowledgeBaseName": "${KB_NAME}",
  "includeArchives": ${INCLUDE_ARCHIVES}
}
JSON
