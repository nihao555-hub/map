#!/usr/bin/env bash
# 一键部署（含多节点代理）：在服务器 /home/ubuntu/gmaps 下执行
# 需要环境变量 SUB_URL；可选 NODE / MULTI / NODE_FILTER / SOCKS_PASS / APT_MIRROR
set -eu

cd "$(dirname "$0")"
mkdir -p deploy/xray

export SOCKS_USER="${SOCKS_USER:-gmaps}"
export SOCKS_PASS="${SOCKS_PASS:-gmapsproxy}"
export NODE="${NODE:-新加坡-优化3-Gemini}"
export MULTI="${MULTI:-8}"
export NODE_FILTER="${NODE_FILTER:-新加坡,香港,日本,美国,Taiwan,TW,JP,US,SG,HK}"
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
export PLAYWRIGHT_DOWNLOAD_HOST="${PLAYWRIGHT_DOWNLOAD_HOST:-https://cdn.npmmirror.com/binaries/playwright}"
export APT_MIRROR="${APT_MIRROR:-mirrors.tencentyun.com}"
export GMS_WEB_CONCURRENCY="${GMS_WEB_CONCURRENCY:-16}"
export GMS_WEB_BROWSER_POOL_SIZE="${GMS_WEB_BROWSER_POOL_SIZE:-4}"
export GMS_WEB_PAGES_PER_BROWSER="${GMS_WEB_PAGES_PER_BROWSER:-4}"

if [ -z "${SUB_URL:-}" ]; then
  echo "SUB_URL is required (vmess subscription URL)" >&2
  exit 1
fi

echo "[1/4] Generate xray multi-node config (MULTI=$MULTI preferred=$NODE)..."
SUB_URL="$SUB_URL" NODE="$NODE" MULTI="$MULTI" NODE_FILTER="$NODE_FILTER" \
  SOCKS_USER="$SOCKS_USER" SOCKS_PASS="$SOCKS_PASS" \
  python3 deploy/xray/gen_config.py deploy/xray/config.json

PORTS_FILE=deploy/xray/config.json.ports
if [ ! -f "$PORTS_FILE" ]; then
  echo "1080" > "$PORTS_FILE"
fi
PORTS=$(cat "$PORTS_FILE")
PROXY_LIST=""
IFS=',' read -r -a PORT_ARR <<< "$PORTS"
for p in "${PORT_ARR[@]}"; do
  entry="socks5://${SOCKS_USER}:${SOCKS_PASS}@xray:${p}"
  if [ -z "$PROXY_LIST" ]; then
    PROXY_LIST="$entry"
  else
    PROXY_LIST="${PROXY_LIST},${entry}"
  fi
done

echo "[2/4] Write .env with ${#PORT_ARR[@]} proxies..."
cat > .env << EOF
GOPROXY=${GOPROXY}
PLAYWRIGHT_DOWNLOAD_HOST=${PLAYWRIGHT_DOWNLOAD_HOST}
APT_MIRROR=${APT_MIRROR}
GMS_WEB_CONCURRENCY=${GMS_WEB_CONCURRENCY}
GMS_WEB_BROWSER_POOL_SIZE=${GMS_WEB_BROWSER_POOL_SIZE}
GMS_WEB_PAGES_PER_BROWSER=${GMS_WEB_PAGES_PER_BROWSER}
DISABLE_TELEMETRY=1
GMS_PROXIES=${PROXY_LIST}
GRSAI_API_KEY=${GRSAI_API_KEY:-}
GRSAI_API_HOST=${GRSAI_API_HOST:-https://grsaiapi.com}
GRSAI_MODEL=${GRSAI_MODEL:-gemini-3.1-flash-lite}
EOF

if [ -z "${GRSAI_API_KEY:-}" ]; then
  echo "WARNING: GRSAI_API_KEY empty — 中文 AI 翻译不可用（/api/v1/ai-status enabled=false）" >&2
fi

echo "[3/4] Build and start (docker compose)..."
sudo -E docker compose --env-file .env \
  -f docker-compose.deploy.yaml \
  -f docker-compose.proxy.yaml \
  up -d --build

echo "[4/4] Health check..."
sleep 8
sudo docker restart gmaps-nginx-1 >/dev/null 2>&1 || true
sleep 3

PROXY_OK=0
for p in "${PORT_ARR[@]}"; do
  code=$(sudo docker run --rm --network gmaps_default curlimages/curl:8.5.0 \
    -s -o /dev/null -m 20 -w '%{http_code}' \
    -x "socks5h://${SOCKS_USER}:${SOCKS_PASS}@xray:${p}" \
    'https://www.google.com/maps' || echo '000')
  echo "  socks :${p} -> maps HTTP ${code}"
  if [ "$code" = "200" ] || [ "$code" = "301" ] || [ "$code" = "302" ]; then
    PROXY_OK=$((PROXY_OK + 1))
  fi
done

SITE=$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/ || echo '000')
echo "site=${SITE} working_proxies=${PROXY_OK}/${#PORT_ARR[@]}"
sudo docker compose --env-file .env -f docker-compose.deploy.yaml -f docker-compose.proxy.yaml \
  ps --format '{{.Name}} {{.Status}}'

PUBLIC_IP=$(curl -s --max-time 5 ifconfig.me || echo 'SERVER_IP')
echo ""
echo "Public UI: http://${PUBLIC_IP}:8080"
echo "GMS_PROXIES count: ${#PORT_ARR[@]}"
