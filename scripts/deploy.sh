#!/usr/bin/env bash
# 复磐一键部署：本地构建（前端 embed + Linux 二进制）→ 上传 → 重启服务
# 用法：./scripts/deploy.sh
# 前提：本机 Go 1.26+ / Node 22+；SSH 已配置 Host blog（~/.ssh/config）
set -euo pipefail

cd "$(dirname "$0")/.."

echo "==> 1/4 构建前端"
npm --prefix web run build
rm -rf internal/server/webdist
cp -r web/dist internal/server/webdist

echo "==> 2/4 交叉编译 Linux 二进制"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o fupan-linux-amd64 ./cmd/fupan

echo "==> 3/4 上传到服务器"
scp fupan-linux-amd64 blog:/opt/fupan/fupan.new

echo "==> 4/4 原子替换并重启"
ssh blog 'mv /opt/fupan/fupan.new /opt/fupan/fupan && systemctl restart fupan && sleep 2 && systemctl is-active fupan && curl -s -o /dev/null -w "本机健康检查: %{http_code}\n" http://127.0.0.1:8080/api/me'

echo "==> 完成：https://fupan.bg5dux.top"
