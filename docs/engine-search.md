# 智能引擎搜索：复用高 star 开源，不自研爬虫

圈选的三个「客户发现」模块按统一优先级评估：

1. **克隆仍在维护的高 star GitHub 项目**
2. 没有可商用盘子 → **先不自研**
3. 再考虑第三方 API

## 结论

| 模块 | 能不能做 | 采用的盘子 | 不采用 |
|---|---|---|---|
| **智能引擎搜索**（抖音 / TikTok 找人、主页） | **能**，本 PR 已接入 | [davidteather/TikTok-Api](https://github.com/davidteather/TikTok-Api)（6563★ MIT，2026-07 仍在推）关键词搜人；[Johnserf-Seed/f2](https://github.com/Johnserf-Seed/f2)（2603★ Apache-2.0，2026-04）TikTok 作品搜索抽作者、抖音主页 URL→资料 | [NanmiCoder/MediaCrawler](https://github.com/NanmiCoder/MediaCrawler) 62389★、当天仍在推，但是 **非商业学习许可**，不能进商用产品。[Evil0ctal/Douyin_TikTok_Download_API](https://github.com/Evil0ctal/Douyin_TikTok_Download_API) 19352★，主分支 2025-10 后再无推送，且没有关键词搜人接口 |
| **展会获客** | **暂缓** | 无 | 10times 相关仓库均为 0–1★ 且停更，没有可复用盘子。下一步走第三方（Apify 等），不自研 |
| **海关数据** | **已有另一条 PR** | 无高 star OSS | `hughie21/Customs-Crawler` ~12★。PR #12 已接 Kirchner / ImportYeti 第三方提单 API |

## 私信

TikTok-Api **明确不支持登录后写操作**。产品只打开官方主页，由销售在 App/网页里手动点「私信」。不会代发、群发或绕过平台私信接口。

## 启动开源 sidecar

```bash
docker compose -f docker-compose.engine.yaml up -d --build
# 可选：TikTok 网页先搜索一次，把 ms_token 写入环境
export TIKTOK_MS_TOKEN=...
export F2_TIKTOK_COOKIE=...          # f2 搜 TikTok 作品抽作者时更稳
export F2_DOUYIN_COOKIE=...          # 解析抖音主页 URL 时需要
export TIKHUB_API_TOKEN=...          # 抖音「关键词搜人」f2 仍为 🔵，才用第三方
./google_maps_scraper -web
```

打开 `/discover`。侧栏只放本项目三个模块：地图获客 / 智能引擎 / 发开发信。智能引擎的目标是：**用关键词找到已支持社媒上的店主或真人公开主页**，再打开官方页由你手动私信。不挖邮箱、不挖 WhatsApp、不代发。这和网易外贸通智能引擎（挖邮箱 / WhatsApp 再一键营销）不是同一件事。

- 关键词「电动工具」→ 抖音：电动工具小王、盛隆绿巨人、河北喜提工厂、东成旗舰店、大有工具 等，均带 `douyin.com/user/MS4wLjAB…`
- 关键词「power tools」→ TikTok：`@boschpowertools`、`@mjdtpowertools` 等

sidecar 通了以后再叠加 TikTok-Api / f2；Go 服务不自己签抖音/TikTok 接口。

## 抖音关键词搜人的缺口

f2 README 把 `fetch_search_users` 标成 🔵（未完成）。本仓库 **不补自研签名/搜索**。可选：

- 传入抖音主页 URL / `sec_uid`，走 f2 `fetch_user_profile`
- 配置 `TIKHUB_API_TOKEN`（f2 作者团队的商业 API）
