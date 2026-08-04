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
| Hunter.io API | — | `HUNTER_API_KEY` | **带姓名的工作邮箱**（可选） |
| GLEIF LEI API | — | Go 直连 | 法人主体核验（有 LEI 时） |
| Wikidata | — | Go 直连 | 公开创始人/CEO（大品牌） |
| Wikipedia (en/id) | — | Go 直连 | 条目摘要中的创始人/CEO |
| RDAP / [who-dat](https://github.com/Lissy93/who-dat) | — | Go 直连 | 域名注册人/联系邮箱 |
| crt.sh | — | Go 直连 | 证书透明度主机/邮箱 |
| Wayback CDX | — | Go 直连 | 历史 about/team/contact 页 |
| DuckDuckGo HTML | — | Go 直连 | 公开摘要中的高管提及 |
| DNS TXT | — | Go resolver | TXT 中的公开邮箱 |
| AHU (`ahu_lookup.py`) | — | `AHU_PROXY` + Playwright/civic-stack | **印尼董事/监事真名**（可选） |
| [CrossLinked](https://github.com/m8sec/CrossLinked) | ~1.6k | `tools/CrossLinked` + `crosslinked-venv`（CLI） | **按公司枚举 LinkedIn 员工姓名** |
| [Maigret](https://github.com/soxoj/maigret) | ~36k | `tools/maigret` + `osint-extra-venv`（CLI） | **有真名后**做用户名社媒画像 |
| OpenCorporates API | — | Go 直连 | 公司主体（印尼覆盖弱） |

### AHU 是什么？

**不是 GitHub 项目。** AHU = 印尼法律与人权部「一般法律行政总局」公开公司查询站
[ahu.go.id](https://ahu.go.id)（Pencarian Perseroan Terbatas）。  
我们用 `tools/ahu_lookup.py` 去查 **PT 公司董事/监事真名**——这是印尼法人登记数据，比领英更贴本地批发商。  
机房 IP 会被 Cloudflare 拦，必须配住宅代理：`export AHU_PROXY=socks5://...`

```bash
bash tools/install_osint.sh
export HUNTER_API_KEY=...          # 可选，显著提升「姓名+邮箱」
export AHU_PROXY=http://...        # 可选，印尼董事；机房 IP 会被 CF 拦截
```

决策人满意线优先：`AHU` + `CrossLinked/领英X-Ray` + `Hunter` + 官网/katana；有真名后再跑 `Maigret`。AI 只归类，不编造姓名。
