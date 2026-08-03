#!/usr/bin/env bash
# 安装背调 OSINT 依赖（高 star 开源工具）
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"

need() { command -v "$1" >/dev/null || { echo "missing: $1"; exit 1; }; }
need git
need python3

if ! python3 -m venv -h >/dev/null 2>&1; then
  echo "需要 python3-venv（Ubuntu: sudo apt install python3.12-venv）"
  exit 1
fi

# --- theHarvester ---
if [[ ! -d theHarvester/.git ]]; then
  git clone --depth 1 https://github.com/laramies/theHarvester.git
fi
(
  cd theHarvester
  if ! command -v uv >/dev/null; then
    python3 -m pip install --user uv
    export PATH="${HOME}/.local/bin:${PATH}"
  fi
  uv sync
  uv run theHarvester -h >/dev/null
  echo "[ok] theHarvester"
)

# --- SpiderFoot ---
if [[ ! -d spiderfoot/.git ]]; then
  git clone --depth 1 --branch v4.0 https://github.com/smicallef/spiderfoot.git || \
    git clone --depth 1 https://github.com/smicallef/spiderfoot.git
fi
python3 -m venv spiderfoot-venv
# shellcheck disable=SC1091
source spiderfoot-venv/bin/activate
pip install -U pip wheel
grep -vi '^pyyaml' spiderfoot/requirements.txt > /tmp/sf-req.txt
pip install -r /tmp/sf-req.txt 'PyYAML>=6,<7'
python spiderfoot/sf.py -V
deactivate
echo "[ok] SpiderFoot"

# --- holehe / maigret / socialscan + blackbird + Photon ---
python3 -m venv osint-extra-venv
# shellcheck disable=SC1091
source osint-extra-venv/bin/activate
pip install -U pip wheel
pip install holehe maigret socialscan 'aiohttp>=3.12.14'
if [[ ! -d blackbird/.git ]]; then
  git clone --depth 1 https://github.com/p1ngul1n0/blackbird.git
fi
pip install -r blackbird/requirements.txt || true
pip install 'aiohttp>=3.12.14' || true
if [[ ! -d Photon/.git ]]; then
  git clone --depth 1 https://github.com/s0md3v/Photon.git
fi
pip install -r Photon/requirements.txt || true
deactivate
echo "[ok] holehe/maigret/blackbird/Photon"

# --- amass ---
if ! command -v amass >/dev/null && [[ ! -x "${HOME}/go/bin/amass" ]]; then
  go install -v github.com/owasp-amass/amass/v4/...@master || true
fi
echo "[ok] amass (PATH or ~/go/bin/amass)"

cat <<EOF

已覆盖：
- theHarvester (~17k) 域名邮箱/主机
- SpiderFoot (~20k) DNS/WHOIS/主体
- holehe (~11k) 邮箱注册足迹
- maigret (~36k) 用户名社媒枚举
- blackbird (~6k) 邮箱/用户名社媒搜索
- Photon (~11k) 网站爬虫抽邮箱/社媒
- amass (~15k) 被动子域名
- OpenCorporates API 公司主体

EOF
