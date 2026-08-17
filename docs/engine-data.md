# 智能引擎商户库：路径与续跑

本地 SQLite 是公开源（GLEIF / OSM / Wikidata）的离线目录，**不要提交进 git**（约 1GB）。`/tmp` 和未做快照的 Cloud Agent 环境会丢文件。

## 默认路径

| 优先级 | 路径 | 说明 |
|---|---|---|
| 1 | `ENGINE_MERCHANT_DB` | 显式覆盖 |
| 2 | `store/merchants.db` | 本仓库工作区，优于 `/tmp` |
| 3 | `webdata/merchants.db` | 旧默认，若文件还在会自动打开 |
| 4 | `/tmp/gmaps-webdata/merchants.db` | 上一轮临时库，环境一换就没了 |

入库与搜索都走 `engine.ResolveMerchantDB`。Makefile 目标用 `MERCHANT_DB ?= store/merchants.db`。

```bash
mkdir -p store /tmp/merchant-ingest
make ingest-sea                 # 先补东南亚店铺（印尼检索更有用）
make ingest-merchants           # 再灌 GLEIF 全量（数小时）
make ingest-public-max          # 公开源社媒
make ingest-attach-socials      # 缺社媒断点续跑
make ingest-short-video         # 抖音/TikTok 企业号主页（只要链接）
make ingest-short-video-fast    # 两小时：Wikidata 全量号 + 东南亚→中东→欧美
make ingest-sea-tiktok          # 东南亚 TikTok：TikTok-Api / f2 按词搜（要 sidecar）
make ingest-short-video-public  # 公开源能枚举的 TikTok/抖音主页（含名人、Wayback、OSM）
```

抖音/TikTok **没有**公开的「全部企业号」数据包。GitHub 高 star 项目也没有这份包——它们是「给一个词/一条链接，去平台搜或下载」，不是目录。

**公开能一次拉全的，仍然是 Wikidata：**

| 口径 | TikTok P7085 | 抖音 P7120 |
|---|---|---|
| 全部已标注（含名人） | 约 36,195 | 约 311 |
| 非人名（公司/品牌/组织） | 约 14,410 | 约 182 |

| 项目 | star / 许可 | 能干什么 | 本仓库 |
|---|---|---|---|
| Wikidata P7085 / P7120 | 公开知识库 / CC0 | 已标注账号全量导出 | **主方案**，`make ingest-short-video-seed` |
| [davidteather/TikTok-Api](https://github.com/davidteather/TikTok-Api) | 6.5k MIT | 关键词搜 TikTok 用户 | 已接 sidecar `:8091`。机房 IP 上签名接口常空，会回退到公开搜索页抽 `@handle` |
| [Johnserf-Seed/f2](https://github.com/Johnserf-Seed/f2) | 2.6k Apache-2.0 | TikTok 搜作品抽作者；抖音只解析已有主页 | 已接 sidecar `:8092`；没 `F2_TIKTOK_COOKIE` 时 import 就会要 msToken，搜用户仍 🔵 |
| 公开网页索引 | — | `site:tiktok.com/@` / `site:douyin.com/user` | 已接，常被限流 |
| [NanmiCoder/MediaCrawler](https://github.com/NanmiCoder/MediaCrawler) | 6万+ 非商业 | 多平台采集 | **不接** |
| [Evil0ctal/Douyin_TikTok_Download_API](https://github.com/Evil0ctal/Douyin_TikTok_Download_API) | 1.9万 | 解析已有主页/作品 | 无搜人、无全库，不接 |
| drawrowfly/tiktok-scraper | 5k | 旧爬虫 | 2023 停更 |
| Common Crawl | 官方网页库 | 本可扫 `tiktok.com/@` | 最新库该前缀 0 页，平台拦爬虫 |

两小时内最接近全量的办法：先灌 Wikidata 非人名清单，再按 **东南亚 → 中东 → 欧美** 扫本地词。要再往上堆，只能把已接的 TikTok-Api / f2 sidecar 跑起来（cookie），那是按品类抽样，不是第二份全库。

公开源一次灌完（`make ingest-short-video-public`）：

| 源 | 能枚举什么 | 不是什么 |
|---|---|---|
| Wikidata P7085 / P7120 **含名人** | 已标注账号，TikTok 约 3.6 万、抖音约 311 | 平台账号库 |
| Wikidata P856 官网正好是主页 | 漏填 P7085 的公司 | 很少 |
| Internet Archive CDX `tiktok.com/@` | 历史抓到的主页 | 平台全库；垃圾 URL 会丢掉 |
| Internet Archive CDX `douyin.com/user/` | 同上，抖音 | 同上 |
| OSM `contact:tiktok` / `contact:douyin` | 地图上主动填了标签的店 | 全球大约几百到几千 |
| Common Crawl 最新几期 CDX | 公开网页库里的主页 | 平台拦爬虫，经常 0 页 |

这是公开索引的上限，不是东南亚「全部企业号」。

1. Wikidata QLever：公开能拿到的最全一份公司账号标识
2. [davidteather/TikTok-Api](https://github.com/davidteather/TikTok-Api) sidecar（关键词搜用户）
3. [Johnserf-Seed/f2](https://github.com/Johnserf-Seed/f2) sidecar（TikTok 搜作品抽作者；抖音只解析已有主页 URL）
4. 公开网页索引：`site:tiktok.com/@` / `site:douyin.com/user` + 工厂/旗舰店/wholesale

只写入主页链接（`source=tiktok|douyin`）。不代发、不登录、不用 MediaCrawler（非商业许可）。打开 `/directory` 可翻看法律名和已入库企业号。

启动 Web 时：

```bash
export ENGINE_MERCHANT_DB="$PWD/store/merchants.db"
./google_maps_scraper -web -data-folder webdata
```

## 换环境以后怎么恢复

1. **阿里云 OSS / S3**（推荐，换机器也能拉）：先配密钥，再 `make upload-merchants` / `make restore-merchants`。
2. **Cursor 环境快照**：库灌完后对这台机器做 snapshot，下一台 Agent 从该环境启动，`store/merchants.db` 还在。
3. **自带文件**：把 `store/merchants.db` 拷到新机器同一路径，或设 `ENGINE_MERCHANT_DB`。
4. **重新入库**：没有快照就按上面的 `make ingest-*` 重跑。GLEIF zip 缓存在 `/tmp/merchant-ingest/gleif-lei2.csv.zip`。

## 存到 OSS

库约 1GB，不要进 git。阿里云 OSS 走 S3 兼容接口（也可用 AWS S3 / MinIO / R2）：

```bash
export OSS_REGION=oss-cn-shanghai
export OSS_BUCKET=你的桶名
export OSS_ACCESS_KEY_ID=...
export OSS_ACCESS_KEY_SECRET=...
export ENGINE_OSS_ENDPOINT=oss-cn-shanghai.aliyuncs.com
# 海外机器默认走传输加速 oss-accelerate.aliyuncs.com；设 ENGINE_OSS_ACCELERATE=0 可关掉

make upload-merchants     # 先 VACUUM 出一致快照再上传，不打断正在跑的补社媒
make restore-merchants    # 新环境拉回 store/merchants.db
```

当前已上传的对象：`oss://sora-easy-video-refs/engine/merchants.db.gz`（gzip，约 292MB）。新环境：

```bash
# 配好同一套 OSS_* 后
python3 -c "import oss2,os,gzip,shutil; b=oss2.Bucket(oss2.Auth(os.environ['OSS_ACCESS_KEY_ID'], os.environ['OSS_ACCESS_KEY_SECRET']),'https://oss-accelerate.aliyuncs.com',os.environ['OSS_BUCKET']); b.get_object_to_file('engine/merchants.db.gz','store/merchants.db.gz')"
gzip -d -c store/merchants.db.gz > store/merchants.db
```

也认 `ENGINE_OSS_*`。密钥只放环境变量，不写进仓库。

公开源补不齐约 330 万条「只有法律名、没有官网」的 GLEIF 行。Sherlock 不得对这类名字盲探（会刷出空的 Twitch/Pinterest）。只在已有官网或已验证 handle 时探姐妹页。

## 不要做的事

- 不要把 `merchants.db` 推进 git / Git LFS（体积大，且含公开登记数据的本地拷贝）；要持久化用 OSS 或环境快照
- 不要只写 `/tmp/gmaps-webdata` 然后指望下一台 Cloud Agent 还能读到
- 不要绕过 Cloudflare 去刮付费海关站
