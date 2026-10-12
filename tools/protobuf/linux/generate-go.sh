#!/usr/bin/env bash
# 从仓库根目录执行，生成 Go 协议代码到 generated/go。
#   ./tools/protobuf/linux/generate-go.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TOOL="$ROOT/tools/protobuf/linux"
OUT="$ROOT/generated/go"

mkdir -p "$OUT"
"$TOOL/protoc" \
  --proto_path="$ROOT/proto" \
  --proto_path="$TOOL/include" \
  "--plugin=protoc-gen-go=$TOOL/protoc-gen-go" \
  --go_out="$OUT" \
  --go_opt=paths=source_relative \
  "$ROOT/proto/test.proto"

echo "Go code generated in $OUT"
