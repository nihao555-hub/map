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
```

启动 Web 时：

```bash
export ENGINE_MERCHANT_DB="$PWD/store/merchants.db"
./google_maps_scraper -web -data-folder webdata
```

## 换环境以后怎么恢复

1. **Cursor 环境快照**：库灌完后对这台机器做 snapshot，下一台 Agent 从该环境启动，`store/merchants.db` 还在。
2. **自带文件**：把 `store/merchants.db` 拷到新机器同一路径，或设 `ENGINE_MERCHANT_DB`。
3. **重新入库**：没有快照就按上面的 `make ingest-*` 重跑。GLEIF zip 缓存在 `/tmp/merchant-ingest/gleif-lei2.csv.zip`。

公开源补不齐约 330 万条「只有法律名、没有官网」的 GLEIF 行。Sherlock 不得对这类名字盲探（会刷出空的 Twitch/Pinterest）。只在已有官网或已验证 handle 时探姐妹页。

## 不要做的事

- 不要把 `merchants.db` 推进 git / Git LFS（体积大，且含公开登记数据的本地拷贝）
- 不要只写 `/tmp/gmaps-webdata` 然后指望下一台 Cloud Agent 还能读到
- 不要绕过 Cloudflare 去刮付费海关站
