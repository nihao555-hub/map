#!/usr/bin/env bash
# 安装背调依赖：theHarvester + SpiderFoot（官方 GitHub 源码，勿用 PyPI 假包）
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

# --- theHarvester（官方推荐 uv）---
if [[ ! -d theHarvester/.git ]]; then
  git clone --depth 1 https://github.com/laramies/theHarvester.git
fi
(
  cd theHarvester
  if ! command -v uv >/dev/null; then
    python3 -m pip install --user uv
    export PATH="$HOME/.local/bin:$PATH"
  fi
  uv sync
  uv run theHarvester -h >/dev/null
  echo "[ok] theHarvester via: cd $ROOT/theHarvester && uv run theHarvester"
)

# --- SpiderFoot v4.0（独立 venv；放宽 PyYAML 以便在 Python 3.12 安装）---
if [[ ! -d spiderfoot/.git ]]; then
  git clone --depth 1 --branch v4.0 https://github.com/smicallef/spiderfoot.git || \
    git clone --depth 1 https://github.com/smicallef/spiderfoot.git
fi
python3 -m venv spiderfoot-venv
# shellcheck disable=SC1091
source spiderfoot-venv/bin/activate
pip install -U pip wheel
# 跳过旧 pin 的 PyYAML<6（无 3.12 wheel），其余按 requirements 安装
grep -vi '^pyyaml' spiderfoot/requirements.txt > /tmp/sf-req.txt
pip install -r /tmp/sf-req.txt 'PyYAML>=6,<7'
python spiderfoot/sf.py -V
deactivate
echo "[ok] SpiderFoot via: $ROOT/spiderfoot-venv/bin/python $ROOT/spiderfoot/sf.py"

cat <<EOF

能力说明（本地实测）：
- theHarvester：免费源（crtsh/hackertarget）擅长子域名/主机；公开邮箱多需 Hunter 等 API Key
- SpiderFoot：DNS/WHOIS/OpenCorporates 模块可用；全量 passive 扫描过慢，背调只用精简模块+超时
- OpenCorporates：另有 HTTP API（Go 内直连）

EOF
