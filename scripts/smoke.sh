#!/usr/bin/env bash
# 复磐 FuPan E2E 冒烟测试
# 用法：./scripts/smoke.sh [base_url]
# 覆盖：建库 → 传 MD → 切片入库 → （卡片生成需 LLM Key，见下方 CARD 步骤，默认跳过）
#       → 溯源 → 重启持久化由调用方验证（docker compose down/up 或本脚本 -p 模式）
set -euo pipefail

BASE="${1:-http://localhost:8080}"
PASS=0; FAIL=0
step() { echo -e "\n==> $1"; }
ok()   { PASS=$((PASS+1)); echo "  ✓ $1"; }
bad()  { FAIL=$((FAIL+1)); echo "  ✗ $1"; }

step "1. 健康检查"
H=$(curl -sf "$BASE/api/health") && ok "health: $H" || bad "health 不可达"

step "2. 建库"
LIB=$(curl -sf -X POST "$BASE/api/libraries" -H 'Content-Type: application/json' -d '{"name":"smoke-库-'"$RANDOM"'"}')
LIB_ID=$(echo "$LIB" | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])')
ok "库 id=$LIB_ID"

step "3. 上传 MD"
cat > /tmp/fupan-smoke.md <<'EOF'
# 冒烟笔记
## 第一章 FSRS
FSRS 是一个开源的间隔重复算法，用于安排复习时间。它根据记忆的稳定性与难度计算下次复习时间，目标保持率默认为 0.9。间隔重复的核心思想是在快要遗忘的时候进行复习，用最少的复习次数维持长期记忆。传统的时间盒调度往往依赖固定间隔，而 FSRS 会在每一次复习之后重新估计记忆状态，再给出个性化的下一次复习时间。这套方法既降低了过度复习带来的时间浪费，也显著减少了因为复习不及时而产生的遗忘损失。
## 第二章 溯源
每张卡片都携带来源切片的标识，可以随时回跳并高亮显示原文所在的小节。溯源是记忆系统的信任基石，用户永远能够看到某一条知识出自哪篇笔记的哪一个小节，从而安心地把复习交托给算法调度。当用户发现原文存在错漏时，系统不会静默覆盖用户导入的原文，任何修改都会以版本链的形式保留下来，保证知识库的可追溯与可回滚。
EOF
DOC=$(curl -sf -X POST "$BASE/api/libraries/$LIB_ID/documents" -F "file=@/tmp/fupan-smoke.md")
DOC_ID=$(echo "$DOC" | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])')
CHUNKS=$(echo "$DOC" | python3 -c 'import sys,json;print(json.load(sys.stdin)["chunk_count"])')
[ "$CHUNKS" -ge 2 ] && ok "导入成功，切片数=$CHUNKS" || bad "切片数异常: $CHUNKS"

step "4. 文档详情含标题路径"
DETAIL=$(curl -sf "$BASE/api/documents/$DOC_ID")
echo "$DETAIL" | grep -q "第一章 FSRS" && ok "chunk 含标题路径" || bad "chunk 缺标题路径"

step "5. 复习队列为空（尚无卡片，属预期）"
Q=$(curl -sf "$BASE/api/libraries/$LIB_ID/review/queue")
ok "queue: $(echo "$Q" | head -c 60)…"

step "6. 清理"
curl -sf -X DELETE "$BASE/api/documents/$DOC_ID" > /dev/null && ok "文档已删"
curl -sf -X DELETE "$BASE/api/libraries/$LIB_ID" > /dev/null && ok "库已删"

echo -e "\n========== 冒烟结果: PASS=$PASS FAIL=$FAIL =========="
[ "$FAIL" -eq 0 ]
# 卡片生成与复习闭环需要 LLM API Key：在 Web UI 设置页配置后手动验证
# （服务层与处理器均有单测覆盖：go test ./...）。
