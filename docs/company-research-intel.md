# 背调外部情报集成

开启 `-company-research` 后，除了官网多页解析，还会并行跑下列开源能力：

| 能力 | 来源 | 默认 |
|---|---|---|
| 邮箱验证 / 一次性邮箱 / 角色邮箱 / MX | [AfterShip/email-verifier](https://github.com/AfterShip/email-verifier)（内置 disposable 列表，可用 `INTEL_UPDATE_DISPOSABLE=1` 拉 [disposable-email-domains](https://github.com/disposable-email-domains/disposable-email-domains)） | 开 |
| 电话 E.164 | [nyaruka/phonenumbers](https://github.com/nyaruka/phonenumbers) | 开 |
| 技术栈指纹 | [rverton/webanalyze](https://github.com/rverton/webanalyze) + enthec 规则 | 开 |
| 域名 WHOIS + MX→企业邮 | [likexian/whois](https://github.com/likexian/whois) + `net.LookupMX` | 开 |
| 法人 / 股权穿透 | [GLEIF API](https://www.gleif.org/en/lei-data/gleif-api/)（CC0，免 key） | 开 |
| 难抓官网 LLM 抽取 | [crawl4ai](https://github.com/unclecode/crawl4ai) sidecar | 需 URL |
| AI 尽调报告 | [gpt-researcher](https://github.com/assafelovic/gpt-researcher) sidecar | 需 URL |
| 综合 OSINT | [SpiderFoot](https://github.com/smicallef/spiderfoot) sidecar | 需 URL |

## 用法

```bash
# 仅进程内能力（推荐默认）
./google_maps_scraper -company-research -input queries.txt -results out.csv

# 打开 SMTP 探活（慢，易被拦）
./google_maps_scraper -company-research -email-smtp-verify -input queries.txt -results out.csv

# 挂上 Python sidecar
docker compose -f docker-compose.research.yaml up -d
export CRAWL4AI_URL=http://localhost:11235
export RESEARCHER_URL=http://localhost:8000
export SPIDERFOOT_URL=http://localhost:5001
export OPENAI_API_KEY=sk-...
./google_maps_scraper -company-research -crawl4ai-url "$CRAWL4AI_URL" \
  -researcher-url "$RESEARCHER_URL" -spiderfoot-url "$SPIDERFOOT_URL" \
  -input queries.txt -results out.csv
```

## 达不到外贸通的部分

海关提单、全球工商全量、合规 LinkedIn 档案、企业营收——开源没有可用数据源，需采购（见 `docs/b2b-due-diligence-oss-research.md`）。
