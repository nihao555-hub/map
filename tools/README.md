# OSINT 背调依赖（高 star + 高杠杆拼装）

| 项目 | Stars 量级 | 路径/安装 | 对商家背调的作用 |
|------|------------|-----------|------------------|
| [theHarvester](https://github.com/laramies/theHarvester) | ~17k | `tools/theHarvester` | 域名邮箱/主机 |
| [SpiderFoot](https://github.com/smicallef/spiderfoot) | ~20k | `tools/spiderfoot` | DNS/WHOIS/主体 |
| [holehe](https://github.com/megadose/holehe) | ~11k | `osint-extra-venv` | 邮箱注册足迹 |
| [maigret](https://github.com/soxoj/maigret) | ~36k | `osint-extra-venv` | 用户名社媒档案 |
| [blackbird](https://github.com/p1ngul1n0/blackbird) | ~6k | `tools/blackbird` | 邮箱/用户名跨站搜索 |
| [Photon](https://github.com/s0md3v/Photon) | ~11k | `tools/Photon` | 官网浅爬邮箱/社媒 |
| [Amass](https://github.com/owasp-amass/amass) | ~15k | `~/go/bin/amass` | 被动子域名 |
| [katana](https://github.com/projectdiscovery/katana) | ~17k | `~/go/bin/katana` | about/team 深链 + 人名抽取 |
| Hunter.io API | — | `HUNTER_API_KEY` | **带姓名的工作邮箱** |
| GLEIF LEI API | — | Go 直连 | 法人主体核验（有 LEI 时） |
| Wikidata | — | Go 直连 | 公开创始人/CEO（大品牌） |
| AHU (`ahu_lookup.py`) | — | `AHU_PROXY` + Playwright/civic-stack | **印尼董事/监事真名** |
| OpenCorporates API | — | Go 直连 | 公司主体（印尼覆盖弱） |

```bash
bash tools/install_osint.sh
export HUNTER_API_KEY=...          # 可选，显著提升「姓名+邮箱」
export AHU_PROXY=http://...        # 可选，印尼董事；机房 IP 会被 CF 拦截
```

决策人满意线优先：`AHU` + `Hunter` + 官网/katana 证据；其余源做增强。AI 只归类，不编造姓名。
