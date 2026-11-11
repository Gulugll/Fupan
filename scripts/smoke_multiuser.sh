#!/usr/bin/env bash
# 真实 OIDC 部署的双用户隔离验收；不在本地伪造 Casdoor 会话。
# FUPAN_A_COOKIE / FUPAN_B_COOKIE 必须是两个原始 session token，不要包含 Cookie 名称。
# 用法：FUPAN_A_COOKIE=... FUPAN_B_COOKIE=... ./scripts/smoke_multiuser.sh https://fupan.example.com
set -euo pipefail

BASE="${1:-}"
if [[ -z "$BASE" ]]; then
  echo "用法：$0 <base-url>" >&2
  exit 2
fi
BASE="${BASE%/}"
if [[ -z "${FUPAN_A_COOKIE:-}" || -z "${FUPAN_B_COOKIE:-}" ]]; then
  echo "请通过 FUPAN_A_COOKIE 和 FUPAN_B_COOKIE 提供两个不同的原始 session token" >&2
  exit 2
fi
if [[ "$FUPAN_A_COOKIE" == "$FUPAN_B_COOKIE" ]]; then
  echo "A/B session token 必须属于两个不同用户" >&2
  exit 2
fi
for command_name in curl python3; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "缺少依赖：$command_name" >&2
    exit 2
  fi
done

ORIGIN=$(python3 - "$BASE" <<'PY'
import sys
from urllib.parse import urlsplit

value = sys.argv[1]
parsed = urlsplit(value)
if parsed.scheme not in ("http", "https") or not parsed.netloc:
    raise SystemExit("base URL 必须是完整的 http(s) URL")
if parsed.path not in ("", "/") or parsed.query or parsed.fragment or parsed.username or parsed.password:
    raise SystemExit("base URL 只能包含 scheme、host 和可选端口")
print(f"{parsed.scheme}://{parsed.netloc}")
PY
) || exit 2

TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/fupan-multiuser-smoke.XXXXXX")
SMOKE_FILE="$TMP_DIR/smoke.md"
HTTP_BODY="$TMP_DIR/response.json"
printf '# 多用户隔离冒烟\n\n此文档由 smoke_multiuser.sh 创建并会在验收后删除。\n' > "$SMOKE_FILE"

A_LIB_ID=""
B_LIB_ID=""
A_DOC_ID=""
B_DOC_ID=""
A_ORIGINAL_MODEL=""
B_ORIGINAL_MODEL=""
A_MODEL_TOUCHED=0
B_MODEL_TOUCHED=0
RESPONSE=""

step() { printf '\n==> %s\n' "$1"; }
ok() { printf '  通过：%s\n' "$1"; }
fail() { printf '  失败：%s\n' "$1" >&2; exit 1; }

model_payload() {
  python3 -c 'import json,sys; print(json.dumps({"llm.chat_model": sys.argv[1]}))' "$1"
}

request() {
  local user="$1" method="$2" path="$3" expected="$4"
  shift 4
  local token_var="FUPAN_${user}_COOKIE"
  local token="${!token_var}"
  local status
  local args=(
    -sS --connect-timeout 10 --max-time 60
    --output "$HTTP_BODY" --write-out '%{http_code}'
    --request "$method"
    --header "Cookie: __Host-fupan_session=${token}"
  )
  case "$method" in
    GET|HEAD|OPTIONS) ;;
    *) args+=(--header "Origin: ${ORIGIN}") ;;
  esac
  if ! status=$(curl "${args[@]}" "$@" "${BASE}${path}"); then
    fail "${user} ${method} ${path} 请求失败"
  fi
  RESPONSE=$(<"$HTTP_BODY")
  if [[ "$status" != "$expected" ]]; then
    fail "${user} ${method} ${path} 返回 HTTP ${status}，预期 ${expected}"
  fi
}

best_effort_request() {
  local user="$1" method="$2" path="$3"
  shift 3
  local token_var="FUPAN_${user}_COOKIE"
  local token="${!token_var}"
  local args=(
    -sS --connect-timeout 10 --max-time 60 --output /dev/null
    --request "$method"
    --header "Cookie: __Host-fupan_session=${token}"
  )
  case "$method" in
    GET|HEAD|OPTIONS) ;;
    *) args+=(--header "Origin: ${ORIGIN}") ;;
  esac
  curl "${args[@]}" "$@" "${BASE}${path}" >/dev/null 2>&1 || true
}

on_exit() {
  local exit_code=$?
  trap - EXIT
  set +e
  local user doc_var lib_var touched_var original_var payload
  for user in A B; do
    doc_var="${user}_DOC_ID"
    lib_var="${user}_LIB_ID"
    touched_var="${user}_MODEL_TOUCHED"
    original_var="${user}_ORIGINAL_MODEL"
    if [[ -n "${!doc_var}" ]]; then
      best_effort_request "$user" DELETE "/api/documents/${!doc_var}"
    fi
    if [[ -n "${!lib_var}" ]]; then
      best_effort_request "$user" DELETE "/api/libraries/${!lib_var}"
    fi
    if [[ "${!touched_var}" == "1" ]]; then
      payload=$(model_payload "${!original_var}")
      best_effort_request "$user" PUT /api/settings \
        --header 'Content-Type: application/json' --data-binary "$payload"
    fi
  done
  rm -f "$SMOKE_FILE" "$HTTP_BODY"
  rmdir "$TMP_DIR" 2>/dev/null || true
  exit "$exit_code"
}
trap on_exit EXIT

json_id() {
  python3 -c 'import json,sys; value=json.load(sys.stdin).get("id", ""); assert isinstance(value, str) and value; print(value)' <<<"$RESPONSE"
}

json_model() {
  python3 -c 'import json,sys; value=json.load(sys.stdin).get("llm.chat_model", ""); assert isinstance(value, str); print(value)' <<<"$RESPONSE"
}

step "1. 验证两个 OIDC 身份"
request A GET /api/me 200
if ! A_ME_ID=$(json_id); then fail "A 的 /api/me 响应无效"; fi
request B GET /api/me 200
if ! B_ME_ID=$(json_id); then fail "B 的 /api/me 响应无效"; fi
[[ "$A_ME_ID" != "$B_ME_ID" ]] || fail "A/B /api/me 返回相同用户"
[[ "$A_ME_ID" != "local" && "$B_ME_ID" != "local" ]] || fail "目标处于 NoAuth，而非真实 OIDC 模式"
ok "A/B 为不同 OIDC 用户"

RUN_ID="$(date +%s)-$$"
LIB_NAME="multiuser-smoke-${RUN_ID}"
A_MODEL="smoke-model-a-${RUN_ID}"
B_MODEL="smoke-model-b-${RUN_ID}"

step "2. A/B 创建同名知识库"
LIB_PAYLOAD=$(python3 -c 'import json,sys; print(json.dumps({"name": sys.argv[1], "description": "multiuser smoke"}))' "$LIB_NAME")
request A POST /api/libraries 201 --header 'Content-Type: application/json' --data-binary "$LIB_PAYLOAD"
if ! A_LIB_ID=$(json_id); then fail "A 建库响应无效"; fi
request B POST /api/libraries 201 --header 'Content-Type: application/json' --data-binary "$LIB_PAYLOAD"
if ! B_LIB_ID=$(json_id); then fail "B 建库响应无效"; fi
[[ "$A_LIB_ID" != "$B_LIB_ID" ]] || fail "A/B 知识库 ID 意外相同"
ok "同名知识库已分别创建"

step "3. 知识库列表互不可见"
for user in A B; do
  request "$user" GET /api/libraries 200
  own_var="${user}_LIB_ID"
  if [[ "$user" == "A" ]]; then other_var="B_LIB_ID"; else other_var="A_LIB_ID"; fi
  if ! python3 -c '
import json
import sys

name, own, other = sys.argv[1:]
libraries = json.load(sys.stdin)
assert isinstance(libraries, list), "知识库列表不是数组"
ids = [item.get("id") for item in libraries if isinstance(item, dict)]
assert own in ids, "自己的新知识库不可见"
assert other not in ids, "可看到对方知识库 ID"
matching = [item.get("id") for item in libraries if isinstance(item, dict) and item.get("name") == name]
assert matching == [own], "同名知识库列表包含其他用户数据"
' "$LIB_NAME" "${!own_var}" "${!other_var}" <<<"$RESPONSE"
  then
    fail "${user} 的知识库列表未隔离"
  fi
done
ok "列表仅包含本人同名库"

step "4. 模型设置互不串读"
request A GET /api/settings 200
if ! A_ORIGINAL_MODEL=$(json_model); then fail "无法读取 A 原模型设置"; fi
request B GET /api/settings 200
if ! B_ORIGINAL_MODEL=$(json_model); then fail "无法读取 B 原模型设置"; fi

A_MODEL_TOUCHED=1
request A PUT /api/settings 200 --header 'Content-Type: application/json' --data-binary "$(model_payload "$A_MODEL")"
B_MODEL_TOUCHED=1
request B PUT /api/settings 200 --header 'Content-Type: application/json' --data-binary "$(model_payload "$B_MODEL")"
request A GET /api/settings 200
if ! A_EFFECTIVE_MODEL=$(json_model); then fail "无法回读 A 模型设置"; fi
request B GET /api/settings 200
if ! B_EFFECTIVE_MODEL=$(json_model); then fail "无法回读 B 模型设置"; fi
[[ "$A_EFFECTIVE_MODEL" == "$A_MODEL" ]] || fail "A 模型设置未生效或被串改"
[[ "$B_EFFECTIVE_MODEL" == "$B_MODEL" ]] || fail "B 模型设置未生效或被串改"
[[ "$A_EFFECTIVE_MODEL" != "$B_EFFECTIVE_MODEL" ]] || fail "A/B 模型设置相同"
ok "不同模型设置分别生效"

step "5. 双方各上传一个文档"
request A POST "/api/libraries/${A_LIB_ID}/documents" 201 --form "file=@${SMOKE_FILE};filename=smoke.md"
if ! A_DOC_ID=$(json_id); then fail "A 上传响应无效"; fi
request B POST "/api/libraries/${B_LIB_ID}/documents" 201 --form "file=@${SMOKE_FILE};filename=smoke.md"
if ! B_DOC_ID=$(json_id); then fail "B 上传响应无效"; fi
request A GET "/api/documents/${A_DOC_ID}" 200
request B GET "/api/documents/${B_DOC_ID}" 200
ok "本人文档可读取"

step "6. direct document ID 跨用户返回 404"
request B GET "/api/documents/${A_DOC_ID}" 404
request A GET "/api/documents/${B_DOC_ID}" 404
ok "双向 direct-ID 访问均为 404"

step "7. 清理测试文档、知识库并恢复模型设置"
for user in A B; do
  doc_var="${user}_DOC_ID"
  lib_var="${user}_LIB_ID"
  touched_var="${user}_MODEL_TOUCHED"
  original_var="${user}_ORIGINAL_MODEL"
  request "$user" DELETE "/api/documents/${!doc_var}" 204
  printf -v "$doc_var" '%s' ""
  request "$user" DELETE "/api/libraries/${!lib_var}" 204
  printf -v "$lib_var" '%s' ""
  request "$user" PUT /api/settings 200 --header 'Content-Type: application/json' \
    --data-binary "$(model_payload "${!original_var}")"
  printf -v "$touched_var" '%s' "0"
done
ok "测试数据已清理，原模型设置已恢复"

printf '\n多用户 OIDC smoke 全部通过。\n'
