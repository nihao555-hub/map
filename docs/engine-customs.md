# 公开海关数据入库

本地 SQLite：`store/customs.db`（可用 `ENGINE_CUSTOMS_DB` 覆盖）。**不要提交进 git**。

## 先说清楚：没有「全球 2026 逐笔逐公司全库」

公开渠道里**不存在**外贸通那种「60 亿海关库、每笔订单、每个国家都免费」的数据包。各国政策差异很大：

| 口径 | 美国 US | 英国 GB | 印度 IN | 东南亚等 | 联合国 Comtrade |
|---|---|---|---|---|---|
| **公司名** | 有（海运进口商） | 有（进口商/出口商名录） | **不公开** | 基本**不公开** | **无** |
| **逐票/逐单** | 有（海运 BOL，可删名） | **无**（只有月度聚合申报行） | **不公开** | **不公开** | **无** |
| **2026 数据** | 有（Kirchner 2014–至今） | 有（HMRC 最新 bulk 月包，如 202606） | 仅国家×品类聚合 | 仅国家×品类聚合 | 2025 年报已出；2026 视各国报送 |
| **公开批量** | 无政府 API；Kirchner 500 次/天 | **有** uktradeinfo.com ZIP | 付费/政府内网 | 无 | Preview API 有限额 |

付费聚合商（Panjiva、ImportGenius、Trademo、Volza 等）卖的是**商业清洗库**，不是「完全公开」；本仓库**不接**、不绕过 Cloudflare 去刮付费站。

## 本仓库实际灌什么

`make ingest-customs` → `store/customs.db`：

1. **英国 HMRC uktradeinfo**（完全公开 ZIP）
   - 进口商/出口商：公司名、地址、邮编、当月进口/出口的 8 位 HS 编码
   - BDS 进口/出口行：品类×启运国×港口×统计值×净重（**无公司名**，与名录分开两张表）
   - 有披露控制、企业可 opt-out，**不是全量普查**

2. **美国 Kirchner 海运提单**（公开 JSON API，500 请求/IP/天）
   - 按内置外贸品类 + HS 关键词抽样进口商
   - 入库：公司、HS、部分 `latest_shipments` 提单行
   - **仅美国海运进口**；企业可依法删名；无官方 bulk 下载

3. **联合国 Comtrade Preview**（国家×伙伴国×TOTAL 品类，无公司）
   - 约 25 个主要报告国 × 进口/出口
   - 用于「货源国金额」背景，**不是企业名单**

实时搜索仍走 `/customs` 页面的 Kirchner / ImportYeti / Comtrade 联邦检索；本地库用于离线统计与续跑。

## 命令

```bash
make ingest-customs
# 或
go run ./cmd/ingest-customs -db store/customs.db -year 2026

# 只要英国 HMRC（最快、真正 bulk）
go run ./cmd/ingest-customs -skip-us -skip-comtrade

# 只要美国提单抽样（受 500 次/天限制）
go run ./cmd/ingest-customs -skip-uk -skip-comtrade -keywords "furniture,9403"
```

环境变量与线上一致：`ENGINE_KIRCHNER_URL`、`ENGINE_COMTRADE_URL`。

## 表结构（摘要）

- `customs_companies` — 公司（US/GB）
- `customs_company_products` — 公司 × HS × 月份
- `customs_trade_lines` — GB BDS 聚合申报行
- `customs_shipments` — US 提单样本行
- `customs_comtrade_flows` — 国家口径贸易流
- `customs_ingest_runs` — 续跑记录
