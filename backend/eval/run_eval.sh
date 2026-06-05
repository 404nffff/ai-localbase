#!/bin/bash
# 评估系统运行脚本
# 自动从项目根目录的 .env 文件加载环境变量并运行评估

set -e

# 获取脚本所在目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
PROJECT_ROOT="$(cd "$BACKEND_DIR/.." && pwd)"

# 加载 .env 文件
if [ -f "$PROJECT_ROOT/.env" ]; then
    echo "📝 加载环境变量: $PROJECT_ROOT/.env"
    set -a
    source "$PROJECT_ROOT/.env"
    set +a
else
    echo "⚠️  警告: 未找到 .env 文件，使用默认配置"
fi

# 覆盖路径为本地路径（.env 中是 Docker 容器路径）
export STATE_FILE=data/app-state.json
export UPLOAD_DIR=data/uploads
export QDRANT_URL=http://localhost:6333

# 默认参数
DATASET=${DATASET:-eval/data/ground_truth_kb30_v1.json}
OUTPUT_DIR=${OUTPUT_DIR:-eval/results/kb30-baseline}
MOCK_MODE=${MOCK_MODE:-false}
EVAL_KB_ID=${EVAL_KB_ID:-kb-30}
RUN_LABEL=${RUN_LABEL:-$(date +%Y%m%d-%H%M%S)}

# 显示配置信息
echo ""
echo "🔧 评估配置:"
echo "   Dataset:                $DATASET"
echo "   Output:                 $OUTPUT_DIR"
echo "   Mock Mode:              $MOCK_MODE"
echo "   Eval KB ID:             $EVAL_KB_ID"
echo "   Run Label:              $RUN_LABEL"
echo ""
echo "🔧 Qdrant 配置:"
echo "   URL:                    ${QDRANT_URL}"
echo "   Collection Prefix:      ${QDRANT_COLLECTION_PREFIX:-kb_}"
echo "   Vector Size:            ${QDRANT_VECTOR_SIZE:-768}"
echo ""

# 切换到 backend 目录运行
cd "$BACKEND_DIR"

# 运行评估
echo "🚀 开始评估..."
go run ./eval/cmd/ \
  -dataset "$DATASET" \
  -output "$OUTPUT_DIR" \
  -mock="$MOCK_MODE" \
  -eval-kb-id "$EVAL_KB_ID" \
  -run-label "$RUN_LABEL" \
  "$@"

echo ""
echo "✅ 评估完成！"
echo "📊 报告位置: $OUTPUT_DIR/"
