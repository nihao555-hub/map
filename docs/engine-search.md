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

打开 `/discover`。智能引擎和发开发信用深色窄栏；地图获客回退到加窄侧栏之前最新一版（`8126d0e`：顶栏模块导航 + 左侧表单 + 右侧地图）。智能引擎**不是地图搜店**，页面上可选两种模式：

1. **私信模式**（默认）：输入商品关键词，并选择**找买家**（默认，采购商 / 进口商）或**找卖家**（厂家 / 批发），**输入时选国家**。结果表：名称、类型、国家、平台、主页、简介、打开主页。国家优先从主页标题/简介识别，识别不到时用所选市场；抖音/小红书等默认中国。点击「打开主页 / 去私信」在**右侧预览栏**写草稿。系统不代发。
2. **营销模式**（对齐网易外贸通智能引擎的用法，数据走公开页）：按 [s0md3v/Photon](https://github.com/s0md3v/Photon)（约 1.3 万★）的 intel 流程——先检索定位页面，再抓取页面里已公开的 **邮箱 / WhatsApp**。结果表「账号或邮箱 / 网页标题 / 来源链接」。「一键营销」在右侧写信，**不会代发**。Photon 为 GPL，不链进 Go 二进制。

网易外贸通结果页是**相关企业列表**：点公司名看官网、社交主页、联系人邮箱/电话。他们有约 3000 万企业库，公开网页索引做不到同等条数，也没有海关/联系人库。本页用公开社媒主页把同一套列表列出来。

两种模式共用已支持的社媒勾选；营销模式查询条数有上限，避免一次打满公开检索限流。

已支持公开主页解析（无需 sidecar）：TikTok、抖音、Facebook、Instagram、YouTube、LinkedIn、小红书、快手、微博、B站、X、Pinterest、Threads、Telegram、Reddit、Twitch。默认勾选 TikTok / 抖音 / Facebook / Instagram / YouTube / LinkedIn。

- 关键词「电动工具」→ 抖音：电动工具小王、盛隆绿巨人、河北喜提工厂、东成旗舰店、大有工具 等，均带 `douyin.com/user/MS4wLjAB…`
- 关键词「power tools」→ TikTok：`@boschpowertools`、`@mjdtpowertools` 等

找到一家商家后，会再扩同一家的其他社媒主页（越多越好，但仍是公开页上能看到的）：

1. 打开已找到的主页 / 链出的官网，抽出页面上的 Instagram、TikTok、YouTube 等主页（对齐 Photon 的 intel，GPL 不链进二进制）
2. 若账号是拉丁用户名（如 `osdinlighting`），在已勾选平台上探测同名主页（Sherlock / Maigret 的思路）。**不**把 Sherlock/Maigret 整站 400–3000 个站点跑一遍：商家中文名、抖音 `MS4wLjAB…` 对不上用户名，误报会把表撑爆。同名探测会跳过 `shop` / 纯数字这类泛账号，扩出的主页在结果里跟原商家排在一起并标「同源」

sidecar 通了以后再叠加 TikTok-Api / f2；Go 服务不自己签抖音/TikTok 接口。

## 抖音关键词搜人的缺口

f2 README 把 `fetch_search_users` 标成 🔵（未完成）。本仓库 **不补自研签名/搜索**。可选：

- 传入抖音主页 URL / `sec_uid`，走 f2 `fetch_user_profile`
- 配置 `TIKHUB_API_TOKEN`（f2 作者团队的商业 API）
