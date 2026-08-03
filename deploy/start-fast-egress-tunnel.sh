#!/usr/bin/env bash
# 公网追平本地速度：把 VPS nginx 指到「可直连 Google」的爬虫（经 SSH 反向隧道）。
#
# 用法（在能直连 Google 的机器上）：
#   SSHPASS='...' REMOTE=ubuntu@124.222.208.237 LOCAL_PORT=18080 \
#     bash deploy/start-fast-egress-tunnel.sh
#
# 前置：
#   1) 本机已启动：google-maps-scraper -web -addr :18080 -c 24 ...（不要走中国慢代理）
#   2) VPS 已用 deploy/nginx.fast-egress.conf 并 reload nginx
#   3) VPS sshd GatewayPorts clientspecified
set -eu

REMOTE="${REMOTE:-ubuntu@124.222.208.237}"
LOCAL_PORT="${LOCAL_PORT:-18080}"
REMOTE_BIND="${REMOTE_BIND:-172.19.0.1}"
REMOTE_PORT="${REMOTE_PORT:-18081}"

if [ -z "${SSHPASS:-}" ]; then
  echo "SSHPASS is required" >&2
  exit 1
fi

echo "tunnel ${REMOTE_BIND}:${REMOTE_PORT} -> 127.0.0.1:${LOCAL_PORT} via ${REMOTE}"
while true; do
  echo "[$(date -u +%H:%M:%SZ)] starting reverse tunnel"
  sshpass -e ssh -N \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    -o ServerAliveInterval=20 \
    -o ServerAliveCountMax=3 \
    -o ExitOnForwardFailure=yes \
    -R "${REMOTE_BIND}:${REMOTE_PORT}:127.0.0.1:${LOCAL_PORT}" \
    "$REMOTE" || true
  echo "[$(date -u +%H:%M:%SZ)] tunnel died; retry in 3s"
  sleep 3
done
