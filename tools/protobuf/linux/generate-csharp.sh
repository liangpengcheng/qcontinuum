#!/usr/bin/env bash
# 从仓库根目录执行，生成 C# 协议代码到 generated/csharp。
#   ./tools/protobuf/linux/generate-csharp.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TOOL="$ROOT/tools/protobuf/linux"
OUT="$ROOT/generated/csharp"

mkdir -p "$OUT"
"$TOOL/protoc" \
  --proto_path="$ROOT/proto" \
  --proto_path="$TOOL/include" \
  --csharp_out="$OUT" \
  "$ROOT/proto/test.proto"

echo "C# code generated in $OUT"
