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

# --- Maigret（本地克隆 soxoj/maigret，editable 安装）---
if [[ ! -d maigret/.git ]]; then
  git clone --depth 1 https://github.com/soxoj/maigret.git
fi
python3 -m venv osint-extra-venv
# shellcheck disable=SC1091
source osint-extra-venv/bin/activate
pip install -U pip wheel
pip install -e ./maigret holehe socialscan 'aiohttp>=3.12.14'
if [[ ! -d blackbird/.git ]]; then
  git clone --depth 1 https://github.com/p1ngul1n0/blackbird.git
fi
pip install -r blackbird/requirements.txt || true
pip install 'aiohttp>=3.12.14' || true
if [[ ! -d Photon/.git ]]; then
  git clone --depth 1 https://github.com/s0md3v/Photon.git
fi
pip install -r Photon/requirements.txt || true
maigret --version >/dev/null
deactivate
echo "[ok] maigret(local)+holehe/blackbird/Photon"

# --- CrossLinked（本地克隆 m8sec/CrossLinked，独立 venv 避免与 maigret 依赖冲突）---
if [[ ! -d CrossLinked/.git ]]; then
  git clone --depth 1 https://github.com/m8sec/CrossLinked.git
fi
python3 -m venv crosslinked-venv
# shellcheck disable=SC1091
source crosslinked-venv/bin/activate
pip install -U pip wheel
pip install -e ./CrossLinked 'PySocks>=1.7.1' 'requests[socks]'
crosslinked -h >/dev/null
deactivate
echo "[ok] CrossLinked (local CLI)"

# --- amass ---
if ! command -v amass >/dev/null && [[ ! -x "${HOME}/go/bin/amass" ]]; then
  go install -v github.com/owasp-amass/amass/v4/...@master || true
fi
echo "[ok] amass (PATH or ~/go/bin/amass)"

# --- katana（官网深链发现，配合 about/team 页抽人名）---
if ! command -v katana >/dev/null && [[ ! -x "${HOME}/go/bin/katana" ]]; then
  go install -v github.com/projectdiscovery/katana/cmd/katana@latest || true
fi
echo "[ok] katana (PATH or ~/go/bin/katana)"

chmod +x "${ROOT}/ahu_lookup.py" 2>/dev/null || true

cat <<EOF

已覆盖：
- theHarvester (~17k) 域名邮箱/主机
- SpiderFoot (~20k) DNS/WHOIS/主体
- holehe (~11k) 邮箱注册足迹
- maigret (~36k) tools/maigret 本地克隆 CLI
- CrossLinked (~1.6k) tools/CrossLinked 本地克隆 CLI（Bing/Google 员工名）
- blackbird (~6k) 邮箱/用户名社媒搜索
- Photon (~11k) 网站爬虫抽邮箱/社媒
- amass (~15k) 被动子域名
- katana (~17k) about/team 深链
- OpenCorporates / GLEIF / Wikidata（Go 直连）
- Hunter.io（可选 env HUNTER_API_KEY）
- AHU=印尼 ahu.go.id 董事登记（可选 env AHU_PROXY + tools/ahu_lookup.py）

EOF
