#!/usr/bin/env bash
# 一键部署（含代理）：在服务器 /home/ubuntu/gmaps 下执行
# 需要环境变量 SUB_URL；可选 NODE / SOCKS_PASS / APT_MIRROR
set -eu

cd "$(dirname "$0")"
mkdir -p deploy/xray

export SOCKS_USER="${SOCKS_USER:-gmaps}"
export SOCKS_PASS="${SOCKS_PASS:-gmapsproxy}"
export NODE="${NODE:-新加坡-优化3-Gemini}"
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
export PLAYWRIGHT_DOWNLOAD_HOST="${PLAYWRIGHT_DOWNLOAD_HOST:-https://cdn.npmmirror.com/binaries/playwright}"
export APT_MIRROR="${APT_MIRROR:-mirrors.tencentyun.com}"
export GMS_WEB_CONCURRENCY="${GMS_WEB_CONCURRENCY:-4}"
export GMS_WEB_BROWSER_POOL_SIZE="${GMS_WEB_BROWSER_POOL_SIZE:-1}"
export GMS_WEB_PAGES_PER_BROWSER="${GMS_WEB_PAGES_PER_BROWSER:-4}"

if [ -z "${SUB_URL:-}" ]; then
  echo "SUB_URL is required (vmess subscription URL)" >&2
  exit 1
fi

echo "[1/4] Generate xray config from subscription (node=$NODE)..."
SUB_URL="$SUB_URL" NODE="$NODE" SOCKS_USER="$SOCKS_USER" SOCKS_PASS="$SOCKS_PASS" \
  python3 deploy/xray/gen_config.py deploy/xray/config.json

echo "[2/4] Write .env..."
cat > .env << EOF
GOPROXY=${GOPROXY}
PLAYWRIGHT_DOWNLOAD_HOST=${PLAYWRIGHT_DOWNLOAD_HOST}
APT_MIRROR=${APT_MIRROR}
GMS_WEB_CONCURRENCY=${GMS_WEB_CONCURRENCY}
GMS_WEB_BROWSER_POOL_SIZE=${GMS_WEB_BROWSER_POOL_SIZE}
GMS_WEB_PAGES_PER_BROWSER=${GMS_WEB_PAGES_PER_BROWSER}
DISABLE_TELEMETRY=1
GMS_PROXIES=socks5://${SOCKS_USER}:${SOCKS_PASS}@xray:1080
EOF

echo "[3/4] Build and start (docker compose)..."
sudo -E docker compose --env-file .env \
  -f docker-compose.deploy.yaml \
  -f docker-compose.proxy.yaml \
  up -d --build

echo "[4/4] Health check..."
sleep 8
sudo docker restart gmaps-nginx-1 >/dev/null 2>&1 || true
sleep 3

# Verify proxy can reach Google Maps
PROXY_OK=$(sudo docker run --rm --network gmaps_default curlimages/curl:8.5.0 \
  -s -o /dev/null -m 25 -w '%{http_code}' \
  -x "socks5h://${SOCKS_USER}:${SOCKS_PASS}@xray:1080" \
  'https://www.google.com/maps' || echo '000')

SITE=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/ || echo '000')
echo "site=${SITE} proxy_maps=${PROXY_OK}"
sudo docker compose --env-file .env -f docker-compose.deploy.yaml -f docker-compose.proxy.yaml \
  ps --format '{{.Name}} {{.Status}}'

PUBLIC_IP=$(curl -s --max-time 5 ifconfig.me || echo 'SERVER_IP')
echo ""
echo "Public UI: http://${PUBLIC_IP}:8080"
