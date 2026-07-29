# 前端交接文档

这份文档面向第一次接触本项目的 UI 设计师和前端协作者。目标不是抽象出一个新的设计系统，而是说明当前页面如何工作、哪些视觉区域可以安全调整，以及哪些 DOM 契约不能随意改名。

## 重要前提：这是单页面应用

这是一个 **单页面（single-page）应用**：浏览器加载一个 `index.html`，没有
客户端路由器、没有多页面导航，也没有“查看结果”独立页面。所有交互都是
HTMX partial swap，替换当前页面中的某个区域：

| 操作 | 请求 | 目标区域 | 填充 fragment |
|---|---|---|---|
| 页面加载/轮询统计 | `GET /stats` | `#stats-container` | `stats.html` |
| 页面加载/轮询任务 | `GET /jobs` | `#job-list` | `job_rows.html` |
| 提交任务 | `POST /scrape` | `#job-list`，`afterbegin` | `job_row.html` |
| 运行中进度 | `GET /progress?id=ID` | `#progress-ID`，`outerHTML` | `progress.html` |
| 查看结果 | `GET /view?id=ID` | `#results-container` | `job_view.html` |
| 删除任务 | `DELETE /delete?id=ID` | 最近的任务卡 | 对应任务 fragment |

因此设计师要设计的是**同一页面里的状态转换**，而不是几张相互跳转的
独立 screen：右侧会在空状态、抓取中、已完成、结果表之间变化；“查看结果”
不会导航到新 URL，只会把结果表 swap 到 `#results-container`。浏览器
back/forward、结果页 deep link 和独立结果路由不属于当前交互模型。

## 1. 技术栈现状

当前前端是服务端渲染页面：

- Go `html/template`
- HTMX 1.9.6
- 手写 CSS
- 没有 React、Vue、Tailwind 或前端构建步骤
- 模板和 CSS 通过 Go `go:embed` 编译进二进制

源文件位置：

```text
web/static/templates/index.html       首页和抓取表单
web/static/templates/job_rows.html    /jobs 返回的活动任务、历史任务
web/static/templates/job_row.html     单个任务行的兼容片段
web/static/templates/progress.html    /progress 返回的进度卡
web/static/templates/stats.html       /stats 返回的统计条
web/static/templates/job_view.html    /view 返回的结果表
web/static/css/main.css               全部页面样式和响应式规则
web/static/spec/spec.yaml             OpenAPI 文档
```

`web/web.go` 在 `New` 中解析这些模板，首页由 `index.html` 渲染，其余区域通过 HTMX 请求 HTML fragment。

**重要：每次修改 HTML 模板或 CSS 都必须重新 `go build`。** 浏览器实际加载的是嵌入在二进制中的资源，不是运行时直接读取磁盘文件。重新构建后还要对浏览器做 hard reload，避免旧的 embedded 资源或缓存影响判断。

## 2. 页面结构

当前桌面布局是两列工作区，900px 以下变成单列：

```text
+------------------------------------------------------------------+
| #stats-container  累计任务 / 累计抓取 / 今日任务 / 今日抓取 / 运行中 |
+-----------------------------+------------------------------------+
| .control-column             | .results-column                    |
|                             |                                    |
| .hero-card                  | #job-list                          |
|  - hero-copy                |  - .active-jobs                    |
|  - #scrape-form             |    - .active-job-card              |
|    - #business-type         |    - #progress-ID                  |
|    - #location              |  - .history-section                |
|    - #batch-preview         |    - .history-item                 |
|    - .quantity-picker       |                                    |
|    - .advanced-options      | #results-container                 |
|    - #submit-button         |  - .results-panel                 |
|                             |    - 筛选 / 下载 / 全部列           |
|                             |    - .results-table                |
+-----------------------------+------------------------------------+
```

信息层级必须保持：

1. **开始抓取**：左侧表单和主按钮，是页面最主要动作。
2. **正在进行**：右侧顶部显示实时任务、数量、耗时和最新商家。
3. **历史任务**：右侧列出已完成或失败的任务。
4. **结果**：点击“查看结果”后，结果表加载到右侧 `#results-container`。

没有任务和结果时，右侧显示 `.workspace-empty`，不要让整个右栏看起来像渲染失败。

### 可自由调整的区域

以下内容可以在不改变业务逻辑的情况下改颜色、间距、字号、边框、阴影和布局：

- `.hero-card`、`.control-column`、`.results-column`
- `.stats-strip`
- `.active-job-card`、`.history-item`
- `.progress-card`
- `.results-panel` 及其内部表格样式
- `.chip`、`.button`、表单控件的视觉状态

### 不要随意改名的契约

下面的 id、class、HTMX 属性和 `data-*` 字段被 JavaScript 或服务端引用，改名会破坏交互：

```text
#stats-container
#scrape-form
#business-type
#location
#batch-preview
#manual-keywords
#advanced-lang
#advanced-email
#advanced-fastmode
#submit-button
#spinner
#job-list
#results-container
#results-filter
#toggle-all-columns
#load-more-results
.result-row
.extra-column
.sticky-name
.website-cell
```

结果行上的 `data-title`、`data-category`、`data-address`、`data-phone`、`data-email`、`data-website`、`data-rating`、`data-review-count`、`data-open-hours`、`data-link`、`data-complete-address`、`data-price-range`、`data-descriptions`、`data-thumbnail`、`data-timezone`、`data-plus-code`、`data-latitude`、`data-longitude`、`data-reviews-per-rating`、`data-reservations`、`data-order-online`、`data-menu` 是前端 CSV 导出使用的原始值。不要删除、改名或把它们换成只包含展示文本的值。

## 3. CSS token 清单

token 全部在 `web/static/css/main.css` 的 `:root` 中定义。新样式应优先使用这些 token，不要在组件中重新硬编码同类颜色、间距或圆角。

### 颜色

| Token | 当前值 | 用途 |
|---|---|---|
| `--accent` | `#2563eb` | 主按钮、链接、选中态 |
| `--accent-dark` | `#1d4ed8` | 主按钮 hover、强调文字 |
| `--accent-soft` | `#eff6ff` | 主色浅背景 |
| `--ink` | `#172033` | 正文和标题 |
| `--muted` | `#64748b` | 次要文字、空状态、辅助说明 |
| `--line` | `#e2e8f0` | 边框、分隔线 |
| `--surface` | `#ffffff` | 卡片和输入控件背景 |
| `--surface-raised` | `#f8fafc` | 表头、统计条、进度卡浅层 |
| `--page` | `#f3f7fc` | 页面底色 |
| `--danger` | `#dc2626` | 删除、失败 |
| `--success` | `#15803d` | 成功提示、完成状态 |
| `--warning` | `#b45309` | 警告信息 |

### 间距、字号、圆角、阴影

```css
--space-1: 4px;    --space-2: 8px;
--space-3: 12px;   --space-4: 16px;
--space-5: 24px;   --space-6: 32px;
--space-7: 48px;   --space-8: 64px;

--text-xs: .75rem;
--text-sm: .875rem;
--text-md: 1rem;
--text-lg: 1.25rem;
--text-xl: 1.75rem;
--text-display: clamp(2rem, 5vw, 3.25rem);

--radius-sm: 8px;
--radius-md: 12px;
--radius-lg: 18px;
--radius-xl: 24px;

--shadow-sm: 0 4px 12px rgba(30, 64, 100, .08);
--shadow-lg: 0 20px 55px rgba(30, 64, 100, .09);
--focus-ring: 0 0 0 4px rgba(37, 99, 235, .18);
```

深色系统主题由 `@media (prefers-color-scheme: dark)` 覆盖 `--ink`、`--muted`、`--line`、`--surface`、`--surface-raised`、`--page`、`--accent-soft` 和阴影。设计改动不要只在浅色主题验证。

## 4. 组件和状态

### 表单输入

输入使用 `input[type="text"]`、`input[type="number"]` 和 `textarea`。需要覆盖：

- 默认：白色 surface、边框 `--line`
- hover：可以轻微加强边框
- focus：保持 `focus-visible` 的蓝色 ring
- disabled：降低对比度但仍能读出内容
- 多行 textarea：允许垂直 resize

主要输入是 `#business-type` 和 `#location`。桌面端应保持完整宽度，当前两者已经上下排列。

### 快捷标签 `.chip`

快捷标签写入行业输入框。状态：

- 默认：浅灰背景、细边框、圆角胶囊
- hover：蓝色边框和浅蓝背景
- focus：沿用全局 focus ring
- active：可以用更明显的蓝色背景，但不要改变 `data-value`

标签必须保持紧凑的 flex wrap，避免一个标签占满一行。

### 数量选择 `.quantity-picker` / `.segmented-control`

三个 radio 代表浅、中、深抓取深度。视觉上是 segmented control：

- 默认：浅背景和边框
- checked：蓝色边框、浅蓝背景、强调文字
- focus：radio 或 label 必须仍可键盘聚焦
- disabled：保留可理解的禁用态

不要删除 radio 的共享 `name="quantity"`，否则选择会变成多选。

### 高级选项 `.advanced-options`

使用原生 `<details>` / `<summary>`，可以重新设计箭头、间距和内容网格，但必须保留原生展开能力：

- 默认收起
- hover/focus 的 summary
- 展开后显示语言、邮箱、fast mode、坐标、半径、最长时间、手工关键词和代理
- `#advanced-email` 默认选中

邮箱抓取会访问商家网站，页面已有“更慢”的说明，不应在视觉优化中隐藏。

### 主按钮 `#submit-button`

这是全页主动作，不能被统计条或次要按钮抢夺视觉权重：

- 默认可用：`--accent`
- hover：`--accent-dark`，可有轻微上移
- focus：明显 focus ring
- disabled：灰色背景、不可点击
- loading：`#spinner` 显示，提交期间不要让用户误以为重复创建了任务

### 状态 badge

任务状态使用 `.status-badge` 加 `.status-pending`、`.status-working`、`.status-ok` 或 `.status-failed`。至少要区分：

- 排队中
- 进行中
- 已完成
- 失败

状态不能只依赖颜色，文字也必须保留。

### 进度卡 `.progress-card`

正在运行的任务包含：

- `已抓取 N 条`
- `已运行 mm:ss`
- 进度条
- 最新商家列表

加载中使用 `.progress-loading`。运行态每 2 秒刷新，设计上要避免刷新时高度跳动；最新商家较长时允许截断。

### 历史任务 `.history-item`

历史项展示名称、创建时间、状态、结果数量和操作：

- `查看结果`
- `下载 CSV`
- `删除`

默认、hover、focus 和删除确认态都要清晰。名称可以 ellipsis，但要保留 `title` 完整文本。

### 结果表 `.results-panel`

结果表由 `job_view.html` 唯一渲染，主要能力：

- 默认显示常用列
- 点击 `#toggle-all-columns` 后给 body 加 `show-all-columns`，显示 `.extra-column`
- `.sticky-name` 是 sticky 首列
- `.results-table-wrap` 横向滚动，宽表不能挤坏页面
- `#results-filter` 实时筛选名称和地址
- `#load-more-results` 每次增加 50 行，结果不足或全部加载后必须有 `hidden`
- `.website-cell` 中的网站展示 hostname，但导出仍使用 `data-website` 原始 URL
- 空值显示 muted 的 `—`，导出时为空字符串

结果表不要复制出第二份模板；新列应同时更新 Go `Place`、模板展示和 `exportColumns`。

### 空状态、成功和错误

- `.workspace-empty`：右栏没有任务时的实质性占位区
- `.success-message`：成功后显示，非空时通过 `:not(:empty)` 展示
- `.error-message`：表单或请求错误
- `aria-live="polite"` 用于 `#batch-preview`、`#results-container` 等动态区域
- 错误不能只用红色，应该有明确文字

## 5. HTMX 交互契约

| 元素 | 请求 | 交换方式 | 说明 |
|---|---|---|---|
| `#stats-container` | `GET /stats`，`load, every 15s` | `innerHTML` | 统计条 |
| `#job-list` | `GET /jobs`，`load, every 15s` | `innerHTML` | 活动任务和历史任务 |
| `#scrape-form` | `POST /scrape` | `#job-list` `afterbegin` | 创建一个 job，可包含多个 batch keywords |
| `#progress-ID` | `GET /progress?id=ID`，运行时 `every 2s` | `outerHTML` | 进度卡自我替换 |
| “查看结果” | `GET /view?id=ID` | `#results-container` `innerHTML` | 内嵌结果表 |
| 删除按钮 | `DELETE /delete?id=ID` | 最近任务卡 `outerHTML` | 删除任务 |

`#scrape-form` 的两个 JS hook 不能删除：

```html
hx-on::config-request="configureScrape(event)"
hx-on::after-request="finishScrape(event)"
```

`configureScrape` 负责推断语言、拼接行业 × 城市、设置深度、邮箱和高级参数；`finishScrape` 负责成功/错误反馈和提交状态。只改视觉，不要改 hook 名称。

### 允许改什么、改名会坏什么

可以自由改 CSS、DOM 包裹层和文案，只要上述 id、HTMX 属性、目标和 `data-*` 仍然存在。

以下改动会直接破坏功能：

- 把 `hx-target="#results-container"` 改成不存在的 id
- 删除 `#job-list` 或改变 `/jobs` 的 swap 目标
- 删除 `#progress-ID`，进度轮询无法继续自我替换
- 删除 `.result-row`、`#results-filter` 或 `#load-more-results`
- 删除结果行的导出 `data-*` 属性
- 把 `hx-swap="outerHTML"` 改成会嵌套旧进度卡的方式
- 将 `configureScrape` / `finishScrape` 改名或从表单移除

## 6. 响应式和可访问性

- `900px` 以下 `.workspace` 变成单列，先显示输入，再显示进度、历史和结果。
- 页面最小宽度是 `320px`；390px 宽度下输入和结果表仍可用。
- 结果表允许横向滚动，不要为了“塞下所有列”把文字缩到不可读。
- 使用 `prefers-color-scheme: dark` 的 token 覆盖验证深色系统。
- 使用 `prefers-reduced-motion: reduce` 时，全局动画和滚动动画会降低或关闭。
- `button`、`a`、`input`、`textarea`、`summary` 已有 `:focus-visible` ring。
- 状态、成功、错误和 batch preview 使用可读文字和部分 `aria-live` 区域。
- 空单元格使用 `—`，不能依赖颜色表达“没有数据”。
- 链接应保留可见文本或 `title`，图片有 `alt`，表格首列 sticky 时仍要保证键盘/屏幕阅读器顺序合理。

## 7. 后端接口

浏览器页面接口：

```text
GET  /                 首页
POST /scrape           创建任务
GET  /jobs             任务 HTML fragment
GET  /progress?id=ID  进度 HTML fragment
GET  /stats            统计 HTML fragment
GET  /view?id=ID      结果表 HTML fragment
GET  /download?id=ID  CSV 下载
DELETE /delete?id=ID  删除任务
```

JSON API：

```text
GET  /api/v1/jobs
POST /api/v1/jobs
GET  /api/v1/jobs/{id}
GET  /api/v1/jobs/{id}/progress
GET  /api/v1/jobs/{id}/download
GET  /api/v1/stats
```

完整接口定义：

- 运行时：`/api/docs`
- 源文件：`web/static/spec/spec.yaml`

UI 设计师可以先用真实服务中的 `/jobs`、`/view` fragment 做静态视觉检查；如果需要构造数据，优先通过 `/api/v1/jobs` 创建任务，再观察 `/progress` 和 `/api/v1/stats`。

## 8. 本地预览

Windows PowerShell 示例：

```powershell
$env:Path = "C:\go\bin;$env:Path"
go build -o bin\dev.exe .

New-Item -ItemType Directory -Path webdata-design -Force
.\bin\dev.exe -web -addr ":8090" -data-folder webdata-design
```

浏览器打开：

```text
http://localhost:8090
```

模板/CSS 改动后必须重新执行 `go build`，然后在浏览器执行 hard reload。不要用测试代理正在使用的 `:8080` 或 `bin\google_maps_scraper.exe` 做设计验证。

如果验证部署镜像，`docker-compose.deploy.yaml` 使用仓库根目录的
`Dockerfile` 构建当前代码，而不是拉取上游镜像。这个 Dockerfile 会在构建
阶段下载 Go 依赖并安装 Chromium 及其系统依赖；在 2 vCPU VPS 上属于明显的
一次性重构建成本，通常需要等待数分钟并消耗较多临时磁盘和内存。运行时默认
配置按 2C4G/约 1.5 GiB 容器限制设置，构建和运行不是同一段内存预算。

## 9. 已知视觉待办

以下是当前代码中有意保留、适合 UI 设计师评估的 rough edges：

1. 在“在哪里”和“抓取数量”之间的 batch preview 区域，组合数量和查询预览在多城市时可能占用较多高度。
2. 宽结果表的长文本目前主要通过 ellipsis + `title` 处理，触摸设备没有 hover tooltip。
3. 缩略图列的宽度和加载失败占位仍比较基础。
4. 全部列模式下信息密度很高，横向滚动和 sticky 首列的阴影可以进一步优化。
5. 统计条是低强调设计，但在移动端五项数据会换行，仍可探索更紧凑的展示。
6. 深色模式依赖系统设置，尚未提供页面内主题切换。
7. HTMX fragment 刷新时，设计应避免引入明显的布局跳动和焦点丢失。
