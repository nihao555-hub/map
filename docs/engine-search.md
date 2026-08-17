# 智能引擎搜索：复用高 star 开源，不自研爬虫

圈选的三个「客户发现」模块按统一优先级评估：

1. **克隆仍在维护的高 star GitHub 项目**
2. 没有可商用盘子 → **先不自研**
3. 再考虑第三方 API

## 结论

| 模块 | 能不能做 | 采用的盘子 | 不采用 |
|---|---|---|---|
| **智能引擎搜索**（抖音 / TikTok 找人、主页） | **能**，本 PR 已接入 | 品类店铺：[OpenStreetMap Overpass](https://wiki.openstreetmap.org/wiki/Overpass_API)（`shop=lighting` 全球约 5700 家，ODbL）；公司条目：Wikidata SPARQL。社媒主页仍走公开网页索引 + [davidteather/TikTok-Api](https://github.com/davidteather/TikTok-Api)（6563★ MIT）；[Johnserf-Seed/f2](https://github.com/Johnserf-Seed/f2)（2603★ Apache-2.0） | Apollo 类包装（OpenLeads / KeeLead / DataForge）star 低、本质是 OSM/Wikidata 套皮，不克隆。[NanmiCoder/MediaCrawler](https://github.com/NanmiCoder/MediaCrawler) 非商业许可。OpenCorporates 要 key。GLEIF 按「LED」只有约 200 条且噪声大。Nominatim 4.3k★ 但是 GPL，且不适合一次拉全品类 POI |
| **展会获客** | **能（公开目录 + 知识库）** | [EventsEye](https://www.eventseye.com/) 全球约 1.2 万场实时目录；[LensmorOfficial/trade-show-calendar](https://github.com/LensmorOfficial/trade-show-calendar) 主流展 JSON；AUMA FairFinder；Wikidata SPARQL；公开网页索引 | 没有一家免费的官方全球全量 API。不自研名片 OCR，不链 GPL / 付费爬虫。 |
| **海关数据** | **能（公开提单 + 国家贸易口径）** | [Kirchner](https://www.kirchnerdata.com/llms.txt) 美国海运提单（无 key）；ImportYeti 公开检索；[UN Comtrade preview](https://comtradeapi.un.org/) HS 货源国金额；[世界银行 Indicators](https://api.worldbank.org/v2) 国家商品进出口；USITC HTS 关键词→HS | 没有全球逐票企业库。Census 国际贸易 API 要 key，ACE/Apify/Trademo 为门户或付费，不接。UI 复刻外贸通。 |

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

打开 `/discover`。智能引擎、海关数据、展会获客共用深色窄栏 + 白底「数据获客」子菜单；地图获客仍是顶栏模块导航，不加窄侧栏、不加海关/展会入口。

智能引擎按网易外贸通复刻：空搜是深蓝落地页（WhatsApp / 邮箱 + 精准搜索），搜完后是「邮箱 / 网页标题 / 来源链接」结果表。数据走公开网页索引，不是他们的企业库。营销模式按 [s0md3v/Photon](https://github.com/s0md3v/Photon)（GPL，不链进 Go）的 intel 流程抽公开邮箱 / WhatsApp。「一键营销」只打开系统窗口，**不会代发**。

高级筛选里仍可切到「搜社媒主页」（原私信模式）：找买家/卖家、国家、16 个社媒。点击「打开主页 / 去私信」在右侧预览栏写草稿。系统不代发。

两种模式共用已支持的社媒勾选；营销模式查询条数有上限，避免一次打满公开检索限流。

已支持公开主页解析（无需 sidecar）：TikTok、抖音、Facebook、Instagram、YouTube、LinkedIn、小红书、快手、微博、B站、X、Pinterest、Threads、Telegram、Reddit、Twitch。**默认全部勾选**，可在搜索框下取消。选国家后按地图获客同一套国家→语言表，把商品词展开成当地检索词（如泰国「电动工具」→ power tools / เครื่องมือไฟฟ้า）。本机若有 Ollama（`ENGINE_AI_BASE_URL`，默认 `http://127.0.0.1:11434/v1`）会再补一批当地说法。

公开索引返回的是能解析的社媒主页，不是企业库。一个国家几千家采购商不会都出现在 Facebook/LinkedIn 公开搜索里，也没有海关库。检索慢主要是公开索引限流；已改为按引擎分别限速，不再全局卡 2.8 秒。

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

## 本地商户库

GLEIF / OSM / Wikidata 入库后的 SQLite 默认写 `store/merchants.db`（可用 `ENGINE_MERCHANT_DB` 覆盖）。**不要提交进 git**，也不要只放 `/tmp`——Cloud Agent 换环境会丢。换机器或新 Agent 的恢复步骤见 [engine-data.md](engine-data.md)。
