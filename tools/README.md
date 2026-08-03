# OSINT 背调依赖（尽量全用上的高 star 项目）

| 项目 | Stars 量级 | 路径/安装 | 对商家背调的作用 |
|------|------------|-----------|------------------|
| [theHarvester](https://github.com/laramies/theHarvester) | ~17k | `tools/theHarvester` | 域名邮箱/主机/人名 |
| [SpiderFoot](https://github.com/smicallef/spiderfoot) | ~20k | `tools/spiderfoot` | DNS/WHOIS/主体/内容抽取 |
| [holehe](https://github.com/megadose/holehe) | ~11k | `osint-extra-venv` | 邮箱在哪些站注册过 |
| [maigret](https://github.com/soxoj/maigret) | ~36k | `osint-extra-venv` | 用户名社媒档案 |
| [blackbird](https://github.com/p1ngul1n0/blackbird) | ~6k | `tools/blackbird` | 邮箱/用户名跨站搜索 |
| [Photon](https://github.com/s0md3v/Photon) | ~11k | `tools/Photon` | 官网浅爬邮箱/社媒 |
| [Amass](https://github.com/owasp-amass/amass) | ~15k | `~/go/bin/amass` | 被动子域名/资产面 |
| OpenCorporates API | — | Go 直连 | 公司主体核验 |

```bash
bash tools/install_osint.sh
```

说明：多数「决策人姓名」仍依赖官网证据或付费邮箱 API；免费工具对 SMB 的贡献主要在邮箱/社媒/WHOIS/主体/主机面。
