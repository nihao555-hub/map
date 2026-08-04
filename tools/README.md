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

**不是 GitHub 项目。** AHU = 印尼法律与人权部公开公司查询站
[ahu.go.id/pencarian/profil-pt](https://ahu.go.id/pencarian/profil-pt)。  
`tools/ahu_lookup.py` 用 **Playwright / Crawl4AI / Scrapling** + `AHU_PROXY`（可用 Clash mixed-port）。

| 层级 | 免费？ | 内容 |
|------|--------|------|
| 搜索结果 | ✅ | 公司名、地址、电话、bakum_id、变更历史 |
| Profil Lengkap / Terakhir | ❌ 需付费 voucher | 完整董事/监事 PDF |

机房直连常失败；Clash 新加坡节点实测可搜 AHU：
`export AHU_PROXY=http://127.0.0.1:7890`

LinkedIn X-Ray / Brave 机房 IP 常 403/429；需经 Clash **日本/美国** 节点。
背调会临时切换 `PROXY` 组（搜人用 JP/US，AHU 用新加坡），共用锁避免踩踏：

```bash
bash tools/install_osint.sh
export HUNTER_API_KEY=...          # 可选，显著提升「姓名+邮箱」
export AHU_PROXY=http://127.0.0.1:17890          # Clash mixed-port
export SEARCH_PROXY=http://127.0.0.1:17890        # 与 AHU 同代理即可
export CLASH_API=http://127.0.0.1:19090
export CLASH_DEFAULT_NODE='新加坡SG-HY2'           # AHU
export CLASH_SEARCH_NODE='日本JP-HY2,美国LA-优化-GPT'  # LinkedIn 搜索
```

决策人满意线优先：`AHU` + LinkedIn X-Ray(Brave/Clash) + `CrossLinked` + `Hunter` + 官网/katana；有 `/in/` 后 unavatar 补头像，再跑 `Maigret`。
