# 开发信收发模块

开发信中心把地图获客任务产生的 CSV 客户数据导入三步邮件活动，通过企业邮箱 SMTP 发送，并通过 IMAP 自动识别客户回复、退信和退订。配置 AI 后，每封开发信基于客户官网背调结果、以资深外贸业务口吻单独撰写，客户回信后自动评估购买意向并可生成回信草稿。

> 重要：不存在能“确保高回信率”的文案或发送时间。回复率主要由名单匹配度、产品价值、发件域名信誉、个性化质量和合规性决定。本模块提供可验证的良好默认值，但应先用 30–50 个高度匹配客户测试，再按数据调整。

## 功能

- 从已完成且勾选“抓取邮箱”的地图任务一键导入联系人
- 每个地图商户只选择一个最合适的邮箱（优先官网域名、实名/业务邮箱），避免同一公司多人同时收到
- 每个活动独立去重；退信/退订邮箱进入全局抑制名单
- 三步纯文本序列：首封、3 天后跟进、再过 4 天礼貌结束
- 跟进使用 `In-Reply-To` / `References`，保持在同一个邮件会话
- 按客户 IANA 时区，在当地周二至周四 08:00–11:00 投递
- 新邮箱从 15 封/日起步，每日增加 5 封，硬上限默认 40 封
- 每封间隔随机 90–150 秒，进程重启后仍保留间隔
- IMAP 每两分钟检查一次；回复即停止后续自动邮件
- 识别退信和显式退订，并永久停止该地址
- **AI 逐客户撰写**：抓取客户官网做背调（标题/描述/正文摘要，缓存 30 天），结合评分、城市、行业等真实数据，以 15 年经验外贸 BD 的人设撰写；内置“去 AI 味”校验（拒绝 I hope this email finds you well、delve 等模板腔），禁止编造数字与客户案例，AI 失败自动回退内置模板，发送永不阻塞
- **意向评估**：客户回信先按关键词启发式评分（报价/样品/MOQ→高意向，明确拒绝→无意向，自动回复→待观察），配置 AI 后由模型输出 0–100 分、中文标签与依据；退信/退订自动标记
- **客户工作台**：默认收缩的侧边栏（hover 展开）+ 左侧全部联系人（搜索、按活动/状态筛选、意向徽标）+ 右侧完整收发信历史（气泡时间线）、意向评估卡、商户背景，以及“AI 生成回信草稿 → 人工确认发送”的回信框
- **总览工作台**：一页看全局——活动数、联系人、已触达、回复率、高意向、退信/退订/抑制名单、今日发送配额与 SMTP/IMAP/AI 健康、最近客户来信、各活动表现表
- **批量群发**：在“开发信活动”里勾选多个（或“全部”）已完成的地图任务，一键把抓到的全部客户合并去重后创建为一个活动并群发（每个商户仍只保留一个最优邮箱，命中抑制名单的自动跳过）
- SQLite 本地存储，无外部 CRM 或 SaaS 依赖
- SMTP 与 IMAP 连接测试不会发送测试邮件

## AI 配置（可选，推荐）

兼容任意 OpenAI 格式的 chat-completions 接口：

| 服务 | 接口地址 | 模型示例 |
|---|---|---|
| DeepSeek | `https://api.deepseek.com/v1` | `deepseek-chat` |
| 通义千问 | `https://dashscope.aliyuncs.com/compatible-mode/v1` | `qwen-plus` |
| Kimi (Moonshot) | `https://api.moonshot.cn/v1` | `moonshot-v1-8k` |
| OpenAI | `https://api.openai.com/v1` | `gpt-4o-mini` |
| 本地 Ollama | `http://localhost:11434/v1` | `qwen2.5:14b` |

```bash
export OUTREACH_AI_BASE_URL="https://api.deepseek.com/v1"
export OUTREACH_AI_MODEL="deepseek-chat"
export OUTREACH_AI_API_KEY="sk-..."
```

也可在设置页填写；AI 密钥与邮箱授权码一样只保存在进程内存，重启后需重新输入（推荐环境变量）。

AI 的行为边界（刻意设计）：

- 只允许引用抓取到的真实数据、官网背调摘要和你在活动里填写的价值主张/证据；提示词明确禁止编造数字、客户与认证
- 输出经过“去 AI 味”与长度校验，不合格直接弃用并回退模板
- 客户回信永远不会被 AI 自动回复：AI 只生成草稿，发送必须人工点击确认，价格、承诺、法律相关内容必须人工把关

## 启动

正常启动 Web 模式：

```bash
./google-maps-scraper -web -data-folder webdata
```

访问：

- 地图获客：`http://localhost:8080/`
- 开发信中心：`http://localhost:8080/outreach`

开发信数据保存在 `webdata/outreach.db`。

## 安全须知（务必阅读）

开发信中心复用地图获客的本地 Web 界面，**该界面本身没有登录鉴权**——任何能访问 `-addr` 端口的人都能读写邮箱/AI 设置、查看往来邮件、发信和删除数据。因此：

- 只在**可信的本地或内网环境**运行，或用反向代理加上 HTTP 认证、IP 白名单、VPN 等访问控制后再暴露。
- 不要把启用了邮箱模式的开发信中心直接部署到公网（例如默认的 `render.yaml` 公网服务）。
- 邮箱授权码与 AI 密钥只保存在进程内存、不落库，但配置接口可被有访问权限者修改服务器地址，请务必限制访问来源。
- 假定同一台机器只跑**一个**开发信进程：发送节流与去重依赖单进程内的互斥，未做多进程分布式锁。

## 企业邮箱配置

推荐把客户端授权码放在环境变量，避免写入配置、Shell 历史或 Git：

```bash
export OUTREACH_EMAIL="name@company.example"
export OUTREACH_SMTP_PASSWORD="客户端授权码"
export OUTREACH_FROM_NAME="发件人姓名"
./google-maps-scraper -web -data-folder webdata
```

授权码也可以在 Web 设置页输入，但只保存在当前进程内存，重启后需要重新输入。

### 网易免费企业邮箱（`*.freeqiye.com`）

内置预设：

| 协议 | 主机 | 端口 | 加密 |
|---|---|---:|---|
| SMTP | `smtphz.qiye.163.com` | 465 | 隐式 TLS |
| IMAP | `imaphz.qiye.163.com` | 993 | 隐式 TLS |

需要先在网易企业邮箱后台开启 IMAP/SMTP 和“客户端授权密码”。不要使用网页登录密码。

也可完全使用环境变量覆盖服务器：

```bash
export OUTREACH_SMTP_HOST="smtphz.qiye.163.com"
export OUTREACH_SMTP_PORT="465"
export OUTREACH_IMAP_HOST="imaphz.qiye.163.com"
export OUTREACH_IMAP_PORT="993"
```

## 怎样写更容易得到回复

### 首封结构（建议 60–120 个英文词）

1. **具体而真实的观察**：来自地图资料、网站、评价或客户所在市场，不能编造。
2. **一个相关问题/机会**：说明对方可能关心的结果，不先介绍自己的全部功能。
3. **最小可信证据**：一个量化结果、相似客户或具体做法；没有证据就不要虚构。
4. **单一低摩擦 CTA**：例如“值得用 10 分钟看看是否匹配吗？”，不要同时要求开会、下载附件和访问多个链接。
5. **真实署名与退出方式**：明确身份；让对方可以直接回复退订。

默认模板：

```text
Subject: Quick question about {{.Name}}

Hi {{.Name}} team,

{{.FirstLine}}

We {{.ValueProposition}}.

{{if .Proof}}{{.Proof}}

{{end}}{{.CallToAction}}
If you are the wrong person, I'd appreciate a pointer to the right one.

Best regards,
{{.SenderName}}
{{.SenderCompany}}
```

模板变量包括：

- `{{.Name}}`、`{{.Category}}`、`{{.City}}`
- `{{.Address}}`、`{{.Website}}`、`{{.Phone}}`
- `{{.Rating}}`、`{{.ReviewCount}}`、`{{.FirstLine}}`
- `{{.ValueProposition}}`、`{{.Proof}}`、`{{.CallToAction}}`
- `{{.SenderName}}`、`{{.SenderCompany}}`、`{{.SenderEmail}}`

### 不建议

- “Dear Sir/Madam”加一大段公司介绍
- 虚假的“我一直关注贵司”或编造评价
- 首封放多张图片、附件、短链、跟踪像素和多个 CTA
- “RE:”伪装成已有会话（系统只在真正的跟进邮件使用 `Re:`）
- 紧迫威胁、夸张承诺、全大写、密集感叹号
- 给完全不相关的行业批量发送同一封邮件

## 什么时候发

默认采用收件人当地时间：

- 周二、周三、周四
- 08:00–11:00
- 首次跟进：3 天后
- 最后跟进：再过 4 天

这是 B2B 的合理起点，不是普遍定律。餐饮、零售和跨境客户的工作节奏可能不同。至少比较两组数据：

- 08:00–11:00 vs 13:00–16:00
- 问题型主题 vs 结果型主题
- 只改一个变量，样本量足够后再判断

不要把“打开率”作为唯一目标。隐私保护会让打开追踪失真，本模块不放跟踪像素。优先看有效回复率、正向回复率、退信率和退订率。

## 收到回复后怎么继续

系统收到任意匹配回复后会立即停止自动跟进。建议人工确认后回信：

| 客户回复 | 建议下一步 |
|---|---|
| 有兴趣 | 先回答对方问题，再给 2 个具体会谈时间 |
| 询价 | 给价格区间、适用条件和一个澄清问题，不要只说“电话里谈” |
| 不是负责人 | 感谢并只询问一次正确联系人 |
| 现在不合适 | 询问可否在明确月份再联系；记录时间 |
| 已有供应商 | 问一个差异化问题，不贬低竞争对手 |
| 退订/不要联系 | 不再营销，不争辩；系统自动加入抑制名单 |
| 退信 | 检查地址来源，不要不断重试 |

回信编辑器会设置 `In-Reply-To`，继续在原会话中发送。系统不会自动用 AI 代表公司回复客户，以免在价格、承诺和法律问题上产生未经批准的内容。

## 投递率检查

发送前必须检查：

1. SPF 覆盖实际 SMTP 服务商。
2. DKIM 已在邮箱管理后台启用并能通过验证。
3. `_dmarc.<域名>` 是有效的 `v=DMARC1` 记录。
4. From 地址、DKIM `d=` 域与 SPF Return-Path 尽量对齐。
5. 自定义企业域名有稳定的网站和真实联系方式。
6. 新邮箱逐步升量；硬退信率尽量低于 2%。

### 当前域名诊断（2026-08-12）

对任务提供的邮箱域名进行只读 DNS 检查时：

- MX 指向网易 `hzmx01.mxmail.netease.com`
- SPF 为 `v=spf1 include:spf.163.com -all`
- `_dmarc` 查询返回的是 SPF 字符串，而不是有效的 `v=DMARC1` 记录
- 常见 DKIM selector 查询也没有得到 DKIM 公钥（该免费子域看起来存在通配 TXT）
- 网易 SMTP 465 与 IMAP 993 的 TLS 证书有效且网络可达

因此，当前最大风险不是代码，而是 DMARC/DKIM 与免费共享子域的发件信誉。请在网易管理后台确认 DKIM/DMARC 支持；若平台不允许为该免费子域正确配置，建议换成公司自有域名再正式发开发信。

## 合规

公开显示在网站或地图上的邮箱不等于对营销邮件的普遍同意。发送前需要根据收件人所在地确认适用规则，例如 GDPR/ePrivacy、CAN-SPAM、CASL 和当地反垃圾邮件法律。

最低要求：

- 有明确、可说明的 B2B 相关性和合法联系依据
- 发件人身份、公司名称和联系方式真实
- 不使用欺骗主题、伪造会话或隐藏来源
- 提供简单退出方式，并立即执行
- 记录数据来源、删除/退订请求和抑制名单
- 对个人邮箱、敏感行业和高风险国家先做法律审查

## GitHub 开源方案调研

星标为 2026-08-12 的只读查询结果，会随时间变化。

| 项目 | Stars | 定位 | 是否直接采用 |
|---|---:|---|---|
| [Twenty](https://github.com/twentyhq/twenty) | 54,792 | 开源 CRM | 适合后续 CRM；不负责通用 SMTP/IMAP 冷邮件序列 |
| [listmonk](https://github.com/knadh/listmonk) | 22,738 | 高性能邮件列表/Newsletter | 很成熟，但偏批量广播，不适合 1:1 同线程跟进 |
| [Postal](https://github.com/postalserver/postal) | 16,727 | 自建收发邮件基础设施 | 可替代 SMTP 基础设施，不能替代活动/回复工作流 |
| [Mautic](https://github.com/mautic/mautic) | 10,317 | 完整营销自动化 | 功能强但部署重，地图项目只需一个轻量内嵌模块 |
| [Dittofeed](https://github.com/dittofeed/dittofeed) | 2,894 | 多渠道客户触达 | 偏产品事件/营销消息，不是通用 IMAP 冷邮件会话 |
| [Email-automation](https://github.com/PaulleDemon/Email-automation) | 167 | 专用冷邮件 Web 工具 | 功能方向接近，但 Stars 仍较少且许可证标识不清 |
| [Warmbly](https://github.com/warmbly/warmbly) | 56 | Go 冷邮件与邮箱预热平台 | 方向接近、Apache-2.0，但较新且基础设施明显更重 |
| [cold-cli](https://github.com/andersmyrmel/cold-cli) | 13 | Go + SQLite + SMTP/IMAP 序列引擎 | 架构最贴合但项目很新；采用设计思路而非引入整套程序 |

结论：高 Star 项目主要是 CRM、Newsletter 或邮件服务器，没有一个能直接嵌进当前 Go 地图采集应用，同时满足“CSV 客户导入、1:1 同线程跟进、通用企业 SMTP/IMAP、回复即停止”。因此本模块复用当前项目已有的 Go、SQLite 和 Web UI，新增：

- `github.com/wneessen/go-mail`：SMTP 与 RFC 邮件构造
- `github.com/emersion/go-imap/v2`：IMAP 收件
- `github.com/emersion/go-message`：MIME 正文和邮件头解析

`cold-cli` 的幂等 tick、SQLite 状态、SMTP/IMAP 和同线程序列思路值得借鉴；本实现按当前项目结构重新设计，并增加地图 CSV 导入、客户时区、全局抑制、Web UI 和 API。

## API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/outreach/overview` | 总览聚合数据（活动/联系人/回复/退信/抑制、今日配额、健康状态） |
| GET | `/api/v1/outreach/campaigns` | 活动及统计 |
| POST | `/api/v1/outreach/campaigns` | 从单个地图任务创建活动 |
| POST | `/outreach/campaigns/batch` | 从多个/全部地图任务批量群发（表单，Web UI 使用） |
| GET | `/api/v1/outreach/campaigns/{id}` | 活动和联系人 |
| DELETE | `/api/v1/outreach/campaigns/{id}` | 删除活动 |
| GET | `/api/v1/outreach/contacts` | 工作台联系人列表（含意向；支持 campaign/status/q 筛选） |
| GET | `/api/v1/outreach/contacts/{id}` | 单个客户的完整收发信历史与意向 |
| POST | `/api/v1/outreach/contacts/{id}/suggest` | AI 起草回信（仅草稿，需人工发送） |
| GET | `/api/v1/outreach/settings` | 获取脱敏邮箱/AI 设置 |
| POST | `/api/v1/outreach/settings` | 保存邮箱、发送策略与 AI 设置 |
| POST | `/api/v1/outreach/tick` | 手动执行一次幂等循环 |
| POST | `/api/v1/outreach/reply` | 人工确认后在原会话回信 |

创建活动：

```json
{
  "name": "Berlin dentists",
  "job_id": "地图任务 UUID",
  "value_proposition": "help dental clinics turn more website visitors into booked appointments",
  "proof": "A similar clinic increased qualified bookings by 18% in 60 days.",
  "call_to_action": "Would a quick 10-minute conversation be useful?",
  "start": false
}
```

继续回信：

```json
{
  "contact_id": 123,
  "body": "Thanks for getting back to me..."
}
```
