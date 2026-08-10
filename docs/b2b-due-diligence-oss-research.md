# B2B 企业背调（尽调）管线：开源选型调研报告

> 目标：在现有 Go 版 Google Maps 采集器（输出 name / address / phone / website / reviews）之上，
> 补齐类似**网易外贸通**（waimao.163.com）的线索深挖能力：角色邮箱与个人邮箱、决策人姓名职位、
> LinkedIn 档案、工商登记与法人实体、VAT/税号、海关提单贸易数据、制裁名单筛查、技术栈、
> 社媒档案、企业规模与营收、AI 研究报告。
>
> **数据核验方式**：所有 star 数、语言、License、最后 push 日期均通过 GitHub REST API
> (`gh api repos/<owner>/<repo>`) 于 **2026-08-10** 实测获取，非估算。
> 凡本报告未列出数字者，均显式标注 "未核实"。搜索猜测出来但实际 404 的仓库已在文末列出，避免以讹传讹。

---

## 0. 结论速览（TL;DR）

1. **能开源自建的**：邮箱发现与验证、技术栈指纹、电话号码规范化、域名/子域情报、社媒账号发现、
   网页抓取与 LLM 结构化抽取、实体消歧去重、AI 研究报告生成。这 8 块开源覆盖度**很高**，
   且大量项目是 Go 原生，能直接 `import` 进现有服务。
2. **半开源、数据要另买的**：制裁名单与 PEP 筛查（代码 MIT 开源，**数据商用必须付费授权**）、
   工商登记（只有 **GLEIF LEI** 是真正 CC0 全球开放的，且含"谁拥有谁"的股权层级）。
3. **完全买不到开源方案的**：**海关提单/贸易流水**、**企业营收与员工数**、**LinkedIn 合规批量档案**、
   **邮箱有效性的最后一公里（catch-all 域名）**。这四项是网易外贸通的核心壁垒，必须采购数据。

一句话：**开源能把"网易外贸通"做到约 60–70% 的字段覆盖度，但最贵、最有差异化的贸易数据部分，开源界是空白。**

---

## 1. 邮箱发现 / 排列组合 / 验证

企业背调里最刚需、也是开源覆盖最好的一块。

| 排名 | 仓库 | Stars | 语言 | License | 最后活动 | 说明 |
|---|---|---|---|---|---|---|
| 1 | [AfterShip/email-verifier](https://github.com/AfterShip/email-verifier) | 1,594 | **Go** | **MIT** | 2026-02-26 | **首选**。Go 原生库，直接 `import`，零进程开销。做语法校验、MX 查询、SMTP 探活、一次性邮箱识别、免费邮箱识别、角色邮箱（role-based, 如 info@/sales@）识别、Gravatar 查询。MIT 可商用。 |
| 2 | [reacherhq/check-if-email-exists](https://github.com/reacherhq/check-if-email-exists) | 9,420 | Rust | **AGPL-3.0 / 商业双授权** | 2026-03-17 | 验证质量业界最强（含 catch-all 判定、Yahoo/Hotmail 特化、代理轮换）。自带 HTTP 后端，可当 sidecar 微服务调。**注意：商用闭源必须买商业授权**，其 LICENSE.md 明写 dual license model。 |
| 3 | [disposable-email-domains/disposable-email-domains](https://github.com/disposable-email-domains/disposable-email-domains) | 5,410 | 数据(Python) | **CC0-1.0（公共领域）** | 2026-08-09 | 一次性/临时邮箱域名黑名单，天天更新。纯数据，CC0 无任何限制，直接 embed 进 Go 二进制。**必用**。 |
| 4 | [megadose/holehe](https://github.com/megadose/holehe) | 11,974 | Python | GPL-3.0 | 2024-09-10 | 用"忘记密码"接口反查邮箱在 120+ 站点的注册情况，可反推个人身份。**已近两年未更新**，站点适配大量失效，且 GPL-3.0。仅建议参考思路。 |
| 5 | [Josue87/EmailFinder](https://github.com/Josue87/EmailFinder) | 424 | Python | GPL-3.0 | 2026-07-21 | 基于搜索引擎的域名邮箱发现。体量小，GPL。 |

**Go 集成建议**：`AfterShip/email-verifier` 直接内嵌 + `disposable-email-domains` 列表 embed，
即可覆盖"角色邮箱识别 + 一次性域名过滤 + MX/SMTP 存活"。
若需要 catch-all 精确判定，再把 `reacher` 作为独立 HTTP sidecar 部署（并处理好 AGPL 授权问题）。

**邮箱排列组合（permutation）**：没有找到值得依赖的高星专用库。
这块逻辑极简单（`{first}.{last}@`、`{f}{last}@` 等约 20 种模板 × MX 验证），**建议自研**，
配合 `CrossLinked` 拿到的真实姓名即可。已搜索 "email permutation generator" 无高星结果。

---

## 2. OSINT 人物与公司枚举

| 排名 | 仓库 | Stars | 语言 | License | 最后活动 | 说明 |
|---|---|---|---|---|---|---|
| 1 | [laramies/theHarvester](https://github.com/laramies/theHarvester) | 16,993 | Python | **GPL-2.0-only** | 2026-08-10 | 老牌域名→邮箱/姓名/子域收集，数十个数据源。维护极活跃。GPL-2.0，**当独立 CLI/容器调用可规避传染**（不链接进主程序）。 |
| 2 | [smicallef/spiderfoot](https://github.com/smicallef/spiderfoot) | 20,132 | Python | **MIT** | 2026-04-13 | 200+ 模块的 OSINT 自动化框架，**自带 REST API + Web UI**，最适合当独立微服务被 Go 调用。MIT 商用友好。是本类**综合性最强**的选择。 |
| 3 | [soxoj/maigret](https://github.com/soxoj/maigret) | 36,283 | Python | **MIT** | 2026-08-10 | 用户名 → 3000+ 站点档案聚合，并抽取档案内的关联信息。MIT + 活跃，优于 Sherlock。 |
| 4 | [sherlock-project/sherlock](https://github.com/sherlock-project/sherlock) | 88,652 | Python | **MIT** | 2026-08-10 | 本类 star 之王。仅做"用户名是否存在"，不抽取档案内容，深度不如 Maigret。 |
| 5 | [m8sec/CrossLinked](https://github.com/m8sec/CrossLinked) | 1,578 | Python | GPL-3.0 | 2024-11-26 | **对背调价值极高**：通过搜索引擎抓 LinkedIn 员工姓名，再按邮箱格式模板推导企业邮箱。不登录 LinkedIn，规避风控。但**近两年未更新**，需自行修搜索引擎解析。 |
| 6 | [owasp-amass/amass](https://github.com/owasp-amass/amass) | 14,959 | **Go** | **Apache-2.0** | 2026-07-19 | OWASP 旗舰资产测绘，Go 原生可内嵌。域名维度的组织资产发现。 |
| 7 | [WebBreacher/WhatsMyName](https://github.com/WebBreacher/WhatsMyName) | 2,746 | 数据(Python) | **CC-BY-SA-4.0** | 2026-08-09 | 纯 JSON 站点探测规则库，被 Maigret/Blackbird 等复用。**注意 SA（相同方式共享）条款**对数据衍生品有要求。 |
| 8 | [p1ngul1n0/blackbird](https://github.com/p1ngul1n0/blackbird) | 7,459 | Python | 未检出 LICENSE 文件 | 2025-07-13 | 用户名/邮箱搜档案，带 AI 报告。**仓库根目录无 LICENSE，视为保留所有权利，商用风险高**。 |
| 9 | [lanmaster53/recon-ng](https://github.com/lanmaster53/recon-ng) | 5,839 | Python | GPL-3.0 | 2024-11-01 | 框架经典但已近两年未更新，模块 marketplace 大量失效。**不推荐新项目采用**。 |
| 10 | [soxoj/socid-extractor](https://github.com/soxoj/socid-extractor) | 1,058 | Python | MIT | 2026-08-07 | 从档案页 HTML 抽取隐藏 ID/邮箱/生日等，作为 Maigret 的补充。 |

### LinkedIn 专项（决策人姓名/职位）

| 仓库 | Stars | 语言 | License | 最后活动 | 说明 |
|---|---|---|---|---|---|
| [joeyism/linkedin_scraper](https://github.com/joeyism/linkedin_scraper) | 4,399 | Python | GPL-3.0 | 2026-04-10 | 本类最高星。Selenium 驱动，**需要真实 LinkedIn 账号 cookie**，账号封禁风险高。GPL-3.0。 |
| [cullenwatson/StaffSpy](https://github.com/cullenwatson/StaffSpy) | 324 | Python | **WTFPL** | 2025-06-17 | 按公司抓员工列表（姓名/职位/技能），正好对应"决策人枚举"。License 极宽松。但星少、需登录态。 |

> ⚠️ **合规红线**：`tomquirk/linkedin-api`（常被各类文章推荐）**已 404 下架**，实测不存在。
> LinkedIn 对抓取的诉讼与封号非常激进，且 GDPR 下个人档案属个人数据。
> **强烈建议**：走 `CrossLinked` 这种"搜索引擎侧信道"路线拿姓名+职位，
> 不碰登录态抓取；或直接采购 Proxycurl/People Data Labs 等商业 API。

---

## 3. 网页抓取 / 结构化抽取 / LLM 抽取

| 排名 | 仓库 | Stars | 语言 | License | 最后活动 | 说明 |
|---|---|---|---|---|---|---|
| 1 | [unclecode/crawl4ai](https://github.com/unclecode/crawl4ai) | 77,649 | Python | **Apache-2.0** | 2026-07-30 | **LLM 抓取首选**。输出干净 Markdown，内置 CSS/LLM 双模式结构化抽取（给 schema 直接出 JSON），自带 Docker + REST API。Apache-2.0 商用无忧。 |
| 2 | [D4Vinci/Scrapling](https://github.com/D4Vinci/Scrapling) | 73,346 | Python | **BSD-3-Clause** | 2026-08-10 | 反爬绕过能力极强（Cloudflare/指纹），且**选择器自愈**（页面改版后自动重定位元素）——对长期维护官网抓取管线价值巨大。 |
| 3 | [gocolly/colly](https://github.com/gocolly/colly) | 25,413 | **Go** | **Apache-2.0** | 2026-06-18 | Go 原生爬虫框架，与现有服务同进程，无 sidecar 成本。适合官网多页遍历（关于/联系/团队页）。 |
| 4 | [projectdiscovery/katana](https://github.com/projectdiscovery/katana) | 17,301 | **Go** | **MIT** | 2026-08-05 | Go 原生，支持无头浏览器 + JS 解析的站点爬取，可作库调用。用于枚举官网全部可抓页面。 |
| 5 | [firecrawl/firecrawl](https://github.com/firecrawl/firecrawl) | 164,478 | TypeScript | **AGPL-3.0** | 2026-08-10 | star 最高、能力最全（crawl/scrape/extract/search）。**但 AGPL-3.0**：自建并对外提供服务会触发开源义务，商用需买 Cloud 或商业授权。 |
| 6 | [adbar/trafilatura](https://github.com/adbar/trafilatura) | 6,598 | Python | **Apache-2.0** | 2026-08-04 | 正文抽取学术基准冠军，极轻量、无需 LLM。适合把官网页面降噪后再喂给 LLM，**大幅省 token**。 |
| 7 | [ScrapeGraphAI/Scrapegraph-ai](https://github.com/ScrapeGraphAI/Scrapegraph-ai) | 29,299 | Python | **MIT** | 2026-07-20 | 用图管线 + LLM 做抓取，MIT 友好。 |
| 8 | [PuerkitoBio/goquery](https://github.com/PuerkitoBio/goquery) | 14,974 | **Go** | BSD-3-Clause | 2026-07-31 | Go 版 jQuery 选择器，做 JSON-LD / microdata / mailto 抽取的底座。 |
| 9 | [JohannesKaufmann/html-to-markdown](https://github.com/JohannesKaufmann/html-to-markdown) | 3,776 | **Go** | **MIT** | 2026-08-03 | **Go 原生 HTML→Markdown**，等价于 jina-reader 的本地化实现，喂 LLM 前的标准预处理。 |
| 10 | [Unstructured-IO/unstructured](https://github.com/Unstructured-IO/unstructured) | 15,288 | Python | Apache-2.0 | 2026-08-04 | 处理 PDF/PPT/DOCX（如企业宣传册、产品目录）。 |
| 11 | [browser-use/browser-use](https://github.com/browser-use/browser-use) | 108,567 | Python | MIT | 2026-08-06 | AI 操作浏览器。对背调管线偏重，仅在需要登录/交互式表单时用。 |
| 12 | [chromedp/chromedp](https://github.com/chromedp/chromedp) | 13,238 | **Go** | MIT | 2026-07-14 | Go 原生 CDP 驱动，处理 JS 渲染页面。 |

> ⚠️ `go-shiori/go-readability`（941 stars, MIT）实测状态为 **ARCHIVED（已归档）**，不建议新项目依赖。

---

## 4. 工商登记 / 法人实体数据

**本类是开源覆盖度最差的领域之一，必须清醒认知。**

| 数据源 | 开放性 | 说明 |
|---|---|---|
| **GLEIF LEI（全球法人识别编码）** | ✅ **CC0 1.0，完全免费开放，无需 API key** | **本类唯一真正的开源赢家**。已实测确认 GLEIF 官方 Terms of Use 明写数据以 CC0 发布。提供：Golden Copy 全量文件（XML/CSV/JSON，**每日 3 次更新**）+ Delta 增量文件 + `https://api.gleif.org/api/v1/lei-records` REST API。**关键价值：Level 2 "Who Owns Whom" 关系文件，直接给出直接母公司与最终母公司**——这正是背调里最贵的股权穿透字段。 |
| **UK Companies House** | ✅ 官方免费 API（需免费注册 key） | 英国全量公司注册、董事、股东、财报。是**覆盖度最好的国家级免费注册处**。GitHub 上无高星客户端（实测最高仅 11–34 stars），**建议直接自研 Go client，不要依赖三方小库**。 |
| **OpenCorporates** | ❌ **商业 API** | 全球 2 亿+ 公司，但 API 需付费授权，且明确禁止批量再分发。GitHub 上搜到的均为个人小脚本（最高 23 stars，如 `openc/openc-schema`），**无生产级客户端**。 |
| **EU BRIS / VIES VAT** | ⚠️ 官方 Web 服务，无好用开源库 | VIES 提供欧盟 VAT 号有效性校验 SOAP 接口（免费）。GitHub 上 [se-panfilov/jsvat](https://github.com/se-panfilov/jsvat)（140 stars, MIT）**已 ARCHIVED**，仅做格式校验不做真实性校验。**VAT 格式校验建议自研正则**（27 国规则公开），真实性校验直连 VIES。 |

### 制裁 / PEP 筛查（OpenSanctions 生态）

| 仓库 | Stars | 语言 | 代码 License | 最后活动 | 说明 |
|---|---|---|---|---|---|
| [opensanctions/yente](https://github.com/opensanctions/yente) | 165 | Python | **MIT** | 2026-08-10 | **可自托管的筛查 API 服务**（FastAPI + ElasticSearch），做名称模糊匹配与实体去重。代码 MIT。 |
| [opensanctions/opensanctions](https://github.com/opensanctions/opensanctions) | 783 | Python | **MIT** | 2026-08-10 | 440+ 个制裁/PEP 名单的爬取与标准化管线。 |
| [opensanctions/nomenklatura](https://github.com/opensanctions/nomenklatura) | 260 | Python | **MIT** | 2026-08-05 | 实体解析/去重引擎，FtM 数据模型下的记录匹配。 |
| [alephdata/followthemoney](https://github.com/alephdata/followthemoney) | 283 | Python | **MIT** | 2026-02-28 | OCCRP 的 FtM 实体本体与数据模型，是上述全家桶的 schema 基础。**建议直接采用 FtM 作为我们背调实体的统一数据模型**。 |
| [alephdata/aleph](https://github.com/alephdata/aleph) | 2,414 | JS/Python | **MIT** | 2026-02-20 | OCCRP 调查记者用的实体搜索与跨库关联平台。重型，适合内部分析而非线上管线。 |

> 🔴 **关键授权陷阱（实测确认）**：yente/opensanctions 的**代码是 MIT**，但**数据集是 CC-BY-NC 4.0**。
> OpenSanctions 官方文档 (`/docs/commercial/exemption/`) 明确写道：
> *"Any use inside a for-profit business requires a data license. Compliance screening is a commercial
> use even though it generates no revenue."*
> 即：**只要是营利性公司使用，无论用量多少、是否直接产生收入，都必须购买数据授权**。
> 想在商业产品里内嵌，还需要 Reseller/OEM 授权。**自建 yente 不能规避数据费用。**
>
> ✅ **免费替代**：美国 OFAC SDN 名单、欧盟制裁名单、英国 HMT 名单本身都是**各国政府直接免费发布的公开数据**，
> 可自行抓取解析（这正是 opensanctions 管线做的事）。若只需覆盖 OFAC/EU/UK 三大主名单，
> **自研解析器 + 用 MIT 的 nomenklatura 做匹配算法，是完全合法且零数据成本的路径。**

> ⚠️ `opensanctions/zavod` 实测仅 8 stars 且 **已 ARCHIVED**（2023-07 最后更新），功能已并入 opensanctions 主仓。

---

## 5. 海关 / 提单贸易数据 —— **诚实结论：开源界几乎完全空白**

这是整个调研中**最重要的负面结论**，也是网易外贸通真正的护城河。

### 实测搜索结果

在 GitHub 上以 `bill of lading customs data`、`import export customs scraper`、
`trade data hs code` 等关键词搜索，**返回结果为空或仅有 0–1 star 的个人练习项目**。
**不存在任何可用的开源海关提单数据集或采集器。**

### 为什么不存在？法律与商业成因（实测确认）

- 美国提单数据在法律上**确实是公开的**：依据 19 CFR § 103.31，CBP 通过 AMS 系统按日以 CD-ROM
  形式向公众提供提单交易数据（含提单号、收发货人名址、货描、船名、港口等），公众也可依 FOIA 索取。
- **但**：Panjiva（S&P Global）、ImportGenius 等厂商是通过**持续提交 FOIA 请求 + 按政府成本价订阅 CD-ROM**
  拿到原始数据，再做清洗、公司实体归一、跨年度追踪，然后以**订阅制**售卖，且合同**明确禁止再分发**。
  这条获取链路涉及行政流程与持续成本，**天然无法被开源项目复制**。
- 更麻烦的是：进口商/收货人**可依法申请"manifest confidentiality"**（保密），有效期两年可续期，
  申请后其名址将从公开数据中剔除。所以**即便买到数据，覆盖率也天然有缺口**——
  这是所有厂商（包括网易）共同的上限，不是谁的技术问题。

### 唯一开放的贸易数据：仅到"国家×商品"聚合层级，**没有公司名**

| 数据源 | 开放性 | 粒度 | 对背调的价值 |
|---|---|---|---|
| **UN Comtrade** | ✅ 免费 API（需注册 key，有频率限制） | 国家 × HS 编码 × 年/月 | ❌ **无企业名**。只能做宏观市场判断（如"该国进口这类产品的规模趋势"），**无法做单个企业的背调**。 |
| [uncomtrade/comtradeapicall](https://github.com/uncomtrade/comtradeapicall) | 153 stars, Jupyter, **MIT**, 2026-06-11 | 同上 | UN 官方 Python 客户端，**是本类唯一像样的开源仓库**。 |
| [ropensci/comtradr](https://github.com/ropensci/comtradr) | 83 stars, R, 2026-08-01 | 同上 | R 语言客户端，rOpenSci 出品。 |
| **US Census Bureau Trade API** | ✅ 免费 | 国家 × HS × 港口 | ❌ 同样**无企业名**，仅统计聚合。 |

> **结论**：若产品要提供"该企业进口了什么、从哪家供应商进、量多大"这类外贸通核心卖点，
> **必须采购商业数据**（Panjiva / ImportGenius / ImportYeti / Tendata / 腾道 / 邓白氏等）。
> 开源方案 **0% 覆盖**。这一项建议直接走采购，不要投入自研。

---

## 6. 技术栈 / 平台指纹识别

**重要事实核查（实测确认）**：`github.com/wappalyzer/wappalyzer` **已 404，仓库确实不存在**。
Wappalyzer 于 **2023 年 8 月闭源**，删除仓库、废弃 npm 包，并把原本 GPL-3.0 的指纹库并入商业产品。
社区从最后一个开源版本分叉出以下延续项目：

| 排名 | 仓库 | Stars | 语言 | License | 最后活动 | 说明 |
|---|---|---|---|---|---|---|
| 1 | [enthec/webappanalyzer](https://github.com/enthec/webappanalyzer) | 559 | JSON 数据 | **GPL-3.0** | 2026-08-04 | **社区公认的主力延续分支**。Enthec 公司公开承诺永不转私有。保持与原 Wappalyzer 完全一致的 JSON 结构，**可直接被任何语言消费**——对我们就是"下载 JSON 规则 + 用 Go 自己跑正则"，**纯数据消费不构成 GPL 链接传染**。 |
| 2 | [rverton/webanalyze](https://github.com/rverton/webanalyze) | 1,166 | **Go** | **MIT** | 2026-04-15 | **Go 原生 Wappalyzer 实现**，可作为库直接 `import`，吃 webappanalyzer 的 JSON 规则。**本类对我们最优解**（MIT 代码 + 外部规则数据）。 |
| 3 | [projectdiscovery/httpx](https://github.com/projectdiscovery/httpx) | 10,259 | **Go** | **MIT** | 2026-08-05 | Go 原生多用途 HTTP 探测，可拿标题/状态码/CDN/证书/技术栈，高并发。可作库调用。 |
| 4 | [urbanadventurer/WhatWeb](https://github.com/urbanadventurer/WhatWeb) | 6,771 | Ruby | GPL-2.0 | 2026-04-02 | 1800+ 插件，指纹深度强，但 Ruby + GPL-2.0，需 CLI 方式调用。 |
| 5 | [dochne/wappalyzer](https://github.com/dochne/wappalyzer) | 287 | JS | GPL-3.0 | 2024-11-19 | 另一条分叉线，**已近两年未更新**。 |
| 6 | [HTTPArchive/wappalyzer](https://github.com/HTTPArchive/wappalyzer) | 131 | JS | GPL-3.0 | 2026-08-05 | HTTP Archive 为其月度爬取维护的分支，活跃但只服务自身需求。 |

> ⚠️ **共同天花板**：所有免费方案都源自 2023 年那份指纹库。2023 年识别不出的技术，
> 这些分叉**今天基本依然识别不出**。换分叉改变的是 License 和价格，不是识别上限。
> 对背调场景（识别 Shopify/WooCommerce/Magento 等电商平台判断是否 B2C 卖家）已足够。

---

## 7. 电话号码解析与校验

| 排名 | 仓库 | Stars | 语言 | License | 最后活动 | 说明 |
|---|---|---|---|---|---|---|
| 1 | [nyaruka/phonenumbers](https://github.com/nyaruka/phonenumbers) | 1,590 | **Go** | **MIT** | 2026-08-06 | **首选**。libphonenumber 的 Go 移植，维护活跃（本月仍有提交），元数据紧跟上游。做 E.164 规范化、国家/地区归属、号码类型（固话/手机/免费电话）判定。 |
| 2 | [google/libphonenumber](https://github.com/google/libphonenumber) | 18,190 | C++/Java | **Apache-2.0** | 2026-08-01 | 上游权威源，元数据的真理来源。Go 侧不直接用。 |
| 3 | [ttacon/libphonenumber](https://github.com/ttacon/libphonenumber) | 636 | Go | MIT | 2024-04-08 | 另一 Go 移植，**已两年多未更新，元数据陈旧，不推荐**。 |
| 4 | [sundowndev/phoneinfoga](https://github.com/sundowndev/phoneinfoga) | 17,434 | **Go** | GPL-3.0 | 2026-01-06 | 号码 OSINT（运营商、footprint 搜索），Go 编写但 **GPL-3.0**，商用需以 CLI/容器隔离调用。 |

---

## 8. 域名 / 企业情报基础设施

Go 原生生态在这块极其强势，全部 MIT/Apache，可直接内嵌。

| 排名 | 仓库 | Stars | 语言 | License | 最后活动 | 说明 |
|---|---|---|---|---|---|---|
| 1 | [projectdiscovery/subfinder](https://github.com/projectdiscovery/subfinder) | 14,163 | **Go** | **MIT** | 2026-08-06 | 被动子域枚举，聚合数十个源（含证书透明日志）。可作 Go 库调用。发现企业子品牌/子业务站点。 |
| 2 | [projectdiscovery/dnsx](https://github.com/projectdiscovery/dnsx) | 2,832 | **Go** | **MIT** | 2026-08-10 | 高并发 DNS 工具库。**对背调的杀手级用法：查 MX 判断企业用 Google Workspace / M365 / 腾讯企业邮，查 SPF/DMARC 判断技术成熟度。** |
| 3 | [projectdiscovery/tlsx](https://github.com/projectdiscovery/tlsx) | 1,129 | **Go** | **MIT** | 2026-08-09 | TLS 证书信息抓取，从证书 SAN 里挖关联域名与组织名（O 字段常含法人全称）。 |
| 4 | [likexian/whois](https://github.com/likexian/whois) | 493 | **Go** | **Apache-2.0** | 2026-07-03 | Go 原生 WHOIS 查询（配套 `whois-parser`）。拿域名注册年份判断企业存续时长——**这是判断"是否皮包公司"的强信号**。 |
| 5 | [owasp-amass/amass](https://github.com/owasp-amass/amass) | 14,959 | **Go** | **Apache-2.0** | 2026-07-19 | 见第 2 节，综合资产测绘。 |
| 6 | [lc/gau](https://github.com/lc/gau) | 5,057 | **Go** | MIT | 2026-03-20 | 从 Wayback/CommonCrawl 拿历史 URL，可挖已下线的联系页。 |
| 7 | [aboul3la/Sublist3r](https://github.com/aboul3la/Sublist3r) | 11,017 | Python | GPL-2.0 | 2024-08-02 | 经典但**已两年未更新**，能力被 subfinder 完全覆盖。**不推荐**。 |
| 8 | [crtsh/certwatch_db](https://github.com/crtsh/certwatch_db) | 248 | PLSQL | GPL-3.0 | 2026-05-31 | crt.sh 的后端数据库 schema。实务上**直接查 crt.sh 的公开 HTTP/psql 接口即可**，无需自建。 |

---

## 9. 实体解析 / 跨源去重

线索来自 Maps、官网、注册处、社媒，同一家公司会有多条异名记录，此环节是数据质量的关键。

| 排名 | 仓库 | Stars | 语言 | License | 最后活动 | 说明 |
|---|---|---|---|---|---|---|
| 1 | [moj-analytical-services/splink](https://github.com/moj-analytical-services/splink) | 2,333 | Python | **MIT** | 2026-08-06 | **首选**。英国司法部出品，Fellegi-Sunter 概率匹配，**可在 DuckDB 上跑千万级记录**，无需 Spark。MIT + 活跃维护。可离线批处理，不必进在线链路。 |
| 2 | [dedupeio/dedupe](https://github.com/dedupeio/dedupe) | 4,498 | Python | **MIT** | 2025-07-29 | star 最高，主动学习式人工标注训练，易上手。规模上不如 splink，**近一年更新放缓**。 |
| 3 | [opensanctions/nomenklatura](https://github.com/opensanctions/nomenklatura) | 260 | Python | **MIT** | 2026-08-05 | 专为**公司/人名**优化（法律后缀 GmbH/Ltd/有限公司归一、音译处理），领域契合度最高。 |
| 4 | [zinggAI/zingg](https://github.com/zinggAI/zingg) | 1,236 | Java | **AGPL-3.0** | 2026-08-10 | Spark 级规模，但 **AGPL-3.0，商用需谨慎**，且引入 JVM+Spark 重依赖。 |

> **Go 侧现实做法**：在线链路里先用现成的 `deduper` 包做确定性 key（规范化域名 + E.164 电话 + 地理哈希）快速合并；
> splink/nomenklatura 作为**离线批处理**跑模糊匹配，回写簇 ID。不要把 Python 放进在线热路径。

---

## 10. AI 研究报告生成

| 排名 | 仓库 | Stars | 语言 | License | 最后活动 | 说明 |
|---|---|---|---|---|---|---|
| 1 | [assafelovic/gpt-researcher](https://github.com/assafelovic/gpt-researcher) | 28,908 | Python | **Apache-2.0** | 2026-07-18 | **最贴合"生成尽调报告"场景**。自主搜索→多源聚合→**带引用**的长报告，自带 `gptr-server` 可 HTTP 调用。Apache-2.0 商用友好。**本类首选。** |
| 2 | [langchain-ai/open_deep_research](https://github.com/langchain-ai/open_deep_research) | 12,580 | Python | **MIT** | 2026-08-08 | LangChain 官方 deep research 实现，结构清晰易改造为"公司背调"专用 prompt 管线。MIT。 |
| 3 | [stanford-oval/storm](https://github.com/stanford-oval/storm) | 30,893 | Python | **MIT** | 2025-09-30 | 斯坦福出品，多视角提问生成维基式长文，质量高。**但近一年未更新**。 |
| 4 | [LearningCircuit/local-deep-research](https://github.com/LearningCircuit/local-deep-research) | 8,890 | Python | **MIT** | 2026-08-10 | 强调**本地模型 + 私有数据**，若客户数据不能出境/出网，这是最优解。维护极活跃。 |
| 5 | [crewAIInc/crewAI](https://github.com/crewAIInc/crewAI) | 56,890 | Python | **MIT** | 2026-08-10 | 通用多智能体框架，需自己搭报告逻辑。 |
| 6 | [langchain-ai/langgraph](https://github.com/langchain-ai/langgraph) | 39,354 | Python | **MIT** | 2026-08-09 | 有状态图式编排，适合做**可控、可重试、可审计**的固定尽调流程。比 crewAI 更适合生产。 |
| 7 | [huggingface/smolagents](https://github.com/huggingface/smolagents) | 28,738 | Python | **Apache-2.0** | 2026-07-21 | 极简 agent 框架，依赖少，适合轻量嵌入。 |

> **建议**：报告生成不必引入完整 agent 框架。我们的字段是**结构化且已知**的，
> 直接用固定模板 + 一次 LLM 调用（Go 直连 OpenAI/Claude API）即可，
> 成本、延迟、可控性都远优于 agent。仅在需要"开放式补充调研"时，
> 才把 `gpt-researcher` 作为独立服务挂上去。

---

## 11. 一体化"线索增强 / 销售情报"开源项目 —— **实测：基本不存在**

我以 `lead enrichment`、`sales intelligence open source`、`prospecting leads scraper`、
`OSINT company enrichment API self-hosted`、`b2b leads awesome` 等多组关键词在 GitHub 搜索，
**最高星的结果仅 35 stars**：

| 仓库 | Stars | 语言 | License | 最后活动 |
|---|---|---|---|---|
| [codyschneiderx/waterfall-gtm](https://github.com/codyschneiderx/waterfall-gtm) | 35 | Python | MIT | 2026-08-09 |
| [rqcai200/lead-enrichment-scoring](https://github.com/rqcai200/lead-enrichment-scoring) | 23 | Python | MIT | 2026-07-07 |
| [Lead-Orchestra/awesome-b2b-leads](https://github.com/Lead-Orchestra/awesome-b2b-leads) | 18 | Makefile | other | 2026-08-01 |

**结论（对我们是利好）**：
这个赛道**没有任何成熟开源竞品**。原因很清楚——**这类产品的价值 90% 在数据采购与数据资产，
而不在代码**，所以没人开源。唯一称得上"半成品"的是 `reacher`（邮箱验证，AGPL+商业双授权）
和 `spiderfoot`（OSINT 编排，MIT）。

> 这意味着：**我们必须自己做集成层**，但也意味着**做出来就有壁垒**。
> 现有 Go 采集器 + 本报告的组件拼装，本身就是一个市面上不存在的开源组合。

---

## (A) 推荐最小技术栈

目标：**用最少的组件，拿到最大比例的外贸通式字段**。优先 Go 原生 + 宽松 License。

### 第一层：Go 进程内直接 `import`（零运维成本，全部 MIT/Apache/BSD）

| 组件 | License | 覆盖能力 |
|---|---|---|
| [AfterShip/email-verifier](https://github.com/AfterShip/email-verifier) | MIT | 角色邮箱识别、MX/SMTP 存活、一次性邮箱、免费邮箱判定 |
| [disposable-email-domains](https://github.com/disposable-email-domains/disposable-email-domains) | **CC0** | 一次性域名黑名单（embed 进二进制） |
| [nyaruka/phonenumbers](https://github.com/nyaruka/phonenumbers) | MIT | 电话 E.164 规范化、国家归属、号码类型 |
| [rverton/webanalyze](https://github.com/rverton/webanalyze) + [enthec/webappanalyzer](https://github.com/enthec/webappanalyzer) 规则 | MIT 代码 + GPL 数据 | 技术栈/电商平台指纹（判断是否 B2C 卖家） |
| [projectdiscovery/dnsx](https://github.com/projectdiscovery/dnsx) | MIT | **MX→企业邮箱服务商**、SPF/DMARC 成熟度信号 |
| [likexian/whois](https://github.com/likexian/whois) | Apache-2.0 | 域名注册年限 → 企业存续时长（反皮包公司信号） |
| [projectdiscovery/subfinder](https://github.com/projectdiscovery/subfinder) | MIT | 子域/子品牌站点发现 |
| [gocolly/colly](https://github.com/gocolly/colly) + [goquery](https://github.com/PuerkitoBio/goquery) + [html-to-markdown](https://github.com/JohannesKaufmann/html-to-markdown) | Apache/BSD/MIT | 官网多页遍历、JSON-LD/mailto 抽取、LLM 预处理 |

> 这 8 个组件**全部是 Go 原生 + 商用友好 License**，无需任何 sidecar，
> 已能覆盖：角色邮箱、邮箱可达性、电话规范化、技术栈、社媒链接、企业邮箱服务商、域名年龄、官网结构化信息。

### 第二层：单个 Python sidecar 容器（按需异步调用）

| 组件 | License | 覆盖能力 |
|---|---|---|
| [unclecode/crawl4ai](https://github.com/unclecode/crawl4ai) | Apache-2.0 | 难抓官网的 LLM schema 抽取（决策人姓名/职位、公司简介、成立年份） |
| [assafelovic/gpt-researcher](https://github.com/assafelovic/gpt-researcher) | Apache-2.0 | 最终 AI 尽调报告（带引用） |
| [moj-analytical-services/splink](https://github.com/moj-analytical-services/splink) | MIT | 离线跨源实体去重（DuckDB，不进在线链路） |

### 第三层：外部免费数据源（自研轻量 Go client，不依赖三方小库）

| 数据源 | 授权 | 覆盖能力 |
|---|---|---|
| **GLEIF LEI API / Golden Copy** | **CC0** | 法人全称、注册地址、注册处编号、状态，**+ Level 2 母公司/最终母公司股权穿透** |
| **UK Companies House API** | 免费（需注册 key） | 英国公司注册、董事、财报 |
| **EU VIES** | 免费 | VAT 号真实性校验 |
| **OFAC SDN / EU / UK HMT 官方名单** | 政府公开数据 | 制裁筛查（**自研解析器可完全绕开 OpenSanctions 的 NC 授权费**），匹配算法可用 MIT 的 nomenklatura 思路 |

### 明确建议**不要**采用的

- ❌ `firecrawl`（AGPL-3.0）、`zingg`（AGPL-3.0）、`reacher`（AGPL）——商用授权风险，已有等价宽松替代。
- ❌ `recon-ng`、`Sublist3r`、`ttacon/libphonenumber`、`go-shiori/go-readability`（已归档）——停更或已被替代。
- ❌ `joeyism/linkedin_scraper` / `StaffSpy`——需登录态，封号 + GDPR 双重风险。
- ❌ `blackbird`——无 LICENSE 文件，商用法律风险。
- ❌ 直接内嵌 OpenSanctions **数据**——营利性使用必须付费，自建 yente 不能规避。

---

## (B) 开源无法提供、必须采购的能力清单

| # | 能力 | 开源覆盖度 | 原因 | 建议 |
|---|---|---|---|---|
| 1 | **海关提单 / 进出口流水**（收发货人、货描、HS、船期、交易量） | **0%** | 数据虽依 FOIA 公开，但获取链路是"持续 FOIA 申请 + 按成本价订阅 CBP CD-ROM + 实体归一"，属行政+运营成本，无法开源复制；厂商合同禁止再分发；且进口商可依 19 CFR 103.31 申请保密屏蔽 | **必买**：Panjiva(S&P) / ImportGenius / 腾道 Tendata / 易之家。这是外贸通的核心壁垒 |
| 2 | **全球工商登记全覆盖** | **~10%** | 仅 GLEIF(CC0) 与英国 Companies House 真正开放；OpenCorporates 商业授权且禁批量分发；多数国家注册处无 API 或收费 | GLEIF + UK CH 自建；其余**买** OpenCorporates / 邓白氏 |
| 3 | **企业营收 / 员工规模 / 融资** | **~0%** | 私营企业财务非公开数据，靠厂商调研与建模 | **必买**：ZoomInfo / Apollo / Clearbit / 邓白氏 |
| 4 | **LinkedIn 决策人档案（合规批量）** | **~0%** | 开源抓取工具均需登录态，面临封号 + GDPR + LinkedIn 诉讼风险；`tomquirk/linkedin-api` 已下架 | **必买**：Proxycurl / People Data Labs；或仅用 CrossLinked 侧信道取姓名职位 |
| 5 | **个人邮箱验证的最后一公里（catch-all 域名）** | **~30%** | catch-all 域名对任何地址都回 250，SMTP 层物理上无法判定；商业厂商靠海量历史投递反馈数据库 | **买** ZeroBounce / NeverBounce / Hunter 兜底 |
| 6 | **制裁/PEP 名单（全球 440+ 源）** | 代码 100%，**数据 0%** | 代码 MIT，但数据 CC-BY-NC，营利性使用一律需付费授权 | 自研 OFAC/EU/UK 三大主名单解析（免费）；需全球覆盖则**买** OpenSanctions 商业授权 |
| 7 | **企业信用 / 涉诉 / 舆情** | **~0%** | 各国司法与征信数据均为收费 | **必买**：企查查/天眼查（中国）、邓白氏（海外） |
| 8 | **公司层级贸易伙伴关系图谱** | **0%** | 依赖 #1 提单数据，无提单则无从构建 | 依附于 #1 采购 |

---

## 附录：搜索中出现但**实测不存在（404）**的仓库

以下名称常见于各类博客与"awesome"清单，但实测在 GitHub 上**已不存在**，请勿引用：

- `wappalyzer/wappalyzer` —— **2023 年 8 月闭源，仓库已删除**（本报告第 6 节详述）
- `tomquirk/linkedin-api` —— 404，已下架
- `GLEIF-IT/lei-records`、`Superfilin/companies-house-api`、`jhaals/vat`、
  `ideal-postcodes/vat-validator`、`datasets/un-comtrade`、`aabeling/sublist3r` ——
  均为常见"推荐"但实测 404，属以讹传讹或早年已删

## 附录：状态异常需注意的仓库

| 仓库 | 状态 |
|---|---|
| `go-shiori/go-readability` | **ARCHIVED**（已归档） |
| `opensanctions/zavod` | **ARCHIVED**，8 stars，功能已并入主仓 |
| `se-panfilov/jsvat` | **ARCHIVED** |
| `megadose/holehe` | 2024-09 后停更 |
| `m8sec/CrossLinked` | 2024-11 后停更 |
| `lanmaster53/recon-ng` | 2024-11 后停更 |
| `aboul3la/Sublist3r` | 2024-08 后停更 |
| `ttacon/libphonenumber` | 2024-04 后停更，元数据陈旧 |
| `p1ngul1n0/blackbird` | 无 LICENSE 文件 |
| `stanford-oval/storm` | 2025-09 后停更 |

---

*本报告所有仓库元数据经 GitHub REST API 于 2026-08-10 实测核验。
star 数为当日快照，会随时间变化。*
