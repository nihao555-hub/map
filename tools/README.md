# OSINT 背调依赖

| 项目 | 路径 | 用途 | 能否满足「商家决策人」 |
|------|------|------|------------------------|
| [theHarvester](https://github.com/laramies/theHarvester) | `tools/theHarvester` | 域名邮箱/主机/人名（多源） | **部分**：无 API Key 时主要是主机；邮箱常为空 |
| [SpiderFoot](https://github.com/smicallef/spiderfoot) | `tools/spiderfoot` | DNS/WHOIS/主体/内容抽邮箱人名 | **部分**：精简模块可补 WHOIS/主体；全量扫描太慢，不适合每店跑满 |
| [OpenCorporates API](https://opencorporates.com) | Go 直连 | 公司主体核验 | **是（主体）**，非个人决策人 |

安装：

```bash
bash tools/install_osint.sh
```

运行探测：

```bash
# theHarvester
cd tools/theHarvester && uv run theHarvester -d example.com -b crtsh,hackertarget -l 20 -f /tmp/th-out

# SpiderFoot（精简模块）
tools/spiderfoot-venv/bin/python tools/spiderfoot/sf.py \
  -s example.com -m sfp_dnsresolve,sfp_whois,sfp_crt,sfp_dnsraw,sfp_company,sfp_names,sfp_email,sfp_opencorporates \
  -o json -q
```
