import { useMemo, useState, type ReactNode } from 'react'
import { Loader2, RefreshCw, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

export type DecisionMaker = {
  name?: string
  title?: string
  headline?: string
  location?: string
  email?: string
  phone?: string
  whatsapp?: string
  linkedin?: string
  avatar?: string
  profiles?: string[]
  source?: string
  evidence?: string
  confidence?: string
}

export type PlaceIntel = {
  place_id?: string
  title?: string
  website?: string
  domain?: string
  status?: string
  summary?: string
  note?: string
  error?: string
  org_structure?: Array<{ name?: string; role?: string; parent?: string; evidence?: string }>
  decision_makers?: DecisionMaker[]
  extra_emails?: string[]
  phones?: string[]
  socials?: Record<string, string>
  technologies?: string[]
  company_registry?: {
    name?: string
    company_number?: string
    jurisdiction?: string
    current_status?: string
    incorporation_date?: string
    registry_url?: string
  }
  trade?: {
    source?: string
    role?: string
    name?: string
    profile_url?: string
    phone?: string
    address?: string
    country?: string
    total_shipments?: number
    unique_suppliers?: number
    unique_products?: number
    last_year_total?: number
    newest_month?: string
    date_start?: string
    date_end?: string
    top_suppliers?: Array<{ name?: string; country?: string; shipments?: number; profile_url?: string; first_seen?: string }>
    newest_suppliers?: Array<{ name?: string; country?: string; shipments?: number; first_seen?: string }>
    top_carriers?: Array<{ name?: string; shipments?: number }>
    top_origins?: Array<{ name?: string; country?: string; shipments?: number }>
    top_hs_codes?: Array<{ code?: string; description?: string; shipments?: number }>
    product_terms?: Array<{ code?: string; shipments?: number }>
    port_routes?: Array<{ origin?: string; destination?: string; shipments?: number }>
    yearly_shipments?: Array<{ period?: string; shipments?: number }>
    monthly_shipments?: Array<{ period?: string; shipments?: number }>
    growing_supplier?: { name?: string; shipments?: number; first_seen?: string }
    recent_bols?: Array<{
      date?: string
      shipper?: string
      consignee?: string
      product?: string
      hs_code?: string
      vessel?: string
      carrier?: string
      bill_of_lading?: string
    }>
    summary?: string
  }
  firmographics?: {
    legal_name?: string
    ticker?: string
    cik?: string
    revenue_usd?: number
    revenue_year?: string
    employees?: number
    employees_as_of?: string
    source?: string
    filing_url?: string
  }
  mx_hosts?: string[]
  has_mx?: boolean
  confidence?: string
  sources?: string[]
  provider?: string
}

export type PlaceLite = {
  title: string
  address?: string
  website?: string
  phone?: string
  emails?: string
  link?: string
}

/** 外贸通式六维：概览 / 决策人 / 采购交易 / 供应链 / 主体资质 */
type TabId = 'overview' | 'people' | 'trade' | 'supply' | 'entity'

function isGenericEmail(em: string): boolean {
  const local = em.split('@')[0]?.toLowerCase() || ''
  return (
    /^(info|sales|admin|contact|hello|support|office|mail|privacy|noreply)/.test(local) ||
    /^(info|sales)[-_.]/.test(local)
  )
}

function isNamedDecisionMaker(d: DecisionMaker): boolean {
  const name = (d.name || '').trim()
  if (!name) return false
  if (/^(info|sales|admin|contact|hello|support|office)@/i.test(name)) return false
  return /[a-zA-Z\u4e00-\u9fff]{2,}/.test(name)
}

function confBadge(c?: string) {
  if (!c) return null
  const label = c === 'high' ? '高' : c === 'medium' ? '中' : c === 'low' ? '低' : c
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full px-2 py-0.5 text-[11px] font-semibold',
        c === 'high' && 'bg-emerald-50 text-emerald-700',
        c === 'medium' && 'bg-amber-50 text-amber-700',
        c === 'low' && 'bg-slate-100 text-slate-600',
      )}
    >
      触达信心 {label}
    </span>
  )
}

/** 经营画像只展示公司介绍；去掉历史版本拼进来的海关提单文案。 */
function companyIntroText(intel: PlaceIntel): string {
  let s = (intel.summary || '').trim()
  const markers = ['来源 kirchner', '来源 importyeti', '来源 kirchner:us-bol']
  const lower = s.toLowerCase()
  for (const m of markers) {
    const i = lower.indexOf(m)
    if (i >= 0) {
      const rest = s.slice(i + m.length).trim()
      if (rest) return rest
      s = ''
      break
    }
  }
  if (/^美国海关(进口|供应商)记录/.test(s)) return ''
  return s
}

function ExtLink({ href, children }: { href: string; children: React.ReactNode }) {
  const url = href.startsWith('http') ? href : `https://${href}`
  return (
    <a href={url} target="_blank" rel="noreferrer" className="text-[#2F6BFF] hover:underline break-all">
      {children}
    </a>
  )
}

function KV({ label, children }: { label: string; children: React.ReactNode }) {
  if (!children) return null
  return (
    <div className="grid grid-cols-[88px_1fr] gap-2 border-b border-[#F3F4F6] py-2 text-sm last:border-0">
      <dt className="text-[#9CA3AF]">{label}</dt>
      <dd className="min-w-0 text-[#374151]">{children}</dd>
    </div>
  )
}

function Chip({ children }: { children: React.ReactNode }) {
  return (
    <span className="inline-flex max-w-full items-center rounded-md bg-[#F3F4F6] px-2 py-1 text-xs text-[#374151]">
      {children}
    </span>
  )
}

function Empty({ text }: { text: string }) {
  return <p className="py-3 text-sm text-[#9CA3AF]">{text}</p>
}

type TableCol = { key: string; label: string; num?: boolean; className?: string }

/** 采购交易 / 供应链明细表 */
function DataTable({
  title,
  columns,
  rows,
  tall,
}: {
  title: string
  columns: TableCol[]
  rows: Array<Record<string, ReactNode>>
  tall?: boolean
}) {
  if (!rows.length) return null
  return (
    <section>
      <div className="mb-1.5 flex items-baseline justify-between gap-2">
        <h4 className="text-xs font-semibold text-[#6B7280]">{title}</h4>
        <span className="text-[11px] text-[#9CA3AF]">{rows.length} 条</span>
      </div>
      <div
        className={cn(
          'overflow-auto border border-[#E5E7EB] bg-white',
          tall ? 'max-h-[360px]' : 'max-h-[280px]',
        )}
      >
        <table className="w-full min-w-[420px] border-collapse text-left text-xs leading-snug text-[#374151]">
          <thead className="sticky top-0 z-[1] bg-[#F7F9FC]">
            <tr>
              {columns.map((c) => (
                <th
                  key={c.key}
                  className={cn(
                    'whitespace-nowrap border-b border-[#E5E7EB] px-2.5 py-1.5 font-semibold text-[#6B7280]',
                    c.num && 'text-right',
                    c.className,
                  )}
                >
                  {c.label}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row, i) => (
              <tr key={i} className="hover:bg-[#F8FAFC]">
                {columns.map((c) => (
                  <td
                    key={c.key}
                    className={cn(
                      'border-b border-[#EEF1F5] px-2.5 py-1.5 align-top',
                      c.num && 'text-right tabular-nums whitespace-nowrap',
                      c.className,
                    )}
                  >
                    {row[c.key] ?? '—'}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}

function Metric({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="min-w-0 rounded-xl border border-[#E5E7EB] bg-[#F8FAFC] px-3 py-2.5">
      <div className="text-[11px] font-medium text-[#9CA3AF]">{label}</div>
      <div className="mt-0.5 truncate text-lg font-semibold tabular-nums text-[#111827]">{value}</div>
      {hint ? <div className="mt-0.5 truncate text-[11px] text-[#6B7280]">{hint}</div> : null}
    </div>
  )
}

function PersonCard({ d }: { d: DecisionMaker }) {
  const name = (d.name || '').trim()
  if (!name) return null
  const wa = d.whatsapp || d.phone
  const waHref = wa ? `https://wa.me/${String(wa).replace(/\D/g, '')}` : ''
  return (
    <article className="rounded-xl border border-[#E5E7EB] bg-white p-3.5 shadow-[0_1px_0_rgba(15,23,42,0.03)]">
      <div className="flex gap-3">
        {d.avatar ? (
          <img
            src={d.avatar}
            alt=""
            className="size-11 shrink-0 rounded-full object-cover"
            loading="lazy"
            referrerPolicy="no-referrer"
            onError={(e) => {
              ;(e.target as HTMLImageElement).style.display = 'none'
            }}
          />
        ) : (
          <div className="flex size-11 shrink-0 items-center justify-center rounded-full bg-[#EEF2FF] text-sm font-semibold text-[#2F6BFF]">
            {name.charAt(0).toUpperCase()}
          </div>
        )}
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <strong className="text-sm text-[#111827]">{name}</strong>
            {confBadge(d.confidence)}
          </div>
          {d.title ? <div className="mt-0.5 text-xs text-[#6B7280]">{d.title}</div> : null}
          {d.location ? <div className="text-xs text-[#9CA3AF]">{d.location}</div> : null}
        </div>
      </div>
      <dl className="mt-2 space-y-0">
        {d.email ? (
          <KV label="邮箱">
            <a href={`mailto:${d.email}`} className="text-[#2F6BFF] hover:underline">
              {d.email}
            </a>
          </KV>
        ) : null}
        {d.phone ? <KV label="电话">{d.phone}</KV> : null}
        {d.whatsapp && d.whatsapp !== d.phone ? <KV label="WhatsApp">{d.whatsapp}</KV> : null}
        {d.linkedin ? (
          <KV label="LinkedIn">
            <ExtLink href={d.linkedin}>{d.linkedin.replace(/^https?:\/\//, '')}</ExtLink>
          </KV>
        ) : null}
      </dl>
      <div className="mt-2.5 flex flex-wrap gap-1.5">
        {d.email ? (
          <a
            href={`mailto:${d.email}`}
            className="rounded-md bg-[#EEF2FF] px-2.5 py-1 text-[11px] font-semibold text-[#2F6BFF]"
          >
            邮件
          </a>
        ) : null}
        {waHref ? (
          <a
            href={waHref}
            target="_blank"
            rel="noreferrer"
            className="rounded-md bg-emerald-50 px-2.5 py-1 text-[11px] font-semibold text-emerald-700"
          >
            WhatsApp
          </a>
        ) : null}
        {d.linkedin ? (
          <a
            href={d.linkedin.startsWith('http') ? d.linkedin : `https://${d.linkedin}`}
            target="_blank"
            rel="noreferrer"
            className="rounded-md bg-[#EEF2FF] px-2.5 py-1 text-[11px] font-semibold text-[#2F6BFF]"
          >
            LinkedIn
          </a>
        ) : null}
      </div>
      {d.evidence ? <p className="mt-2 text-[11px] leading-relaxed text-[#9CA3AF]">证据：{d.evidence}</p> : null}
    </article>
  )
}

type Props = {
  place: PlaceLite
  intel: PlaceIntel | 'loading' | undefined
  onClose: () => void
  onRefresh: () => void
  refreshing?: boolean
}

export function IntelPanel({ place, intel, onClose, onRefresh, refreshing }: Props) {
  const [tab, setTab] = useState<TabId>('overview')

  const ready = intel && intel !== 'loading' ? intel : null
  const trade = ready?.trade
  const dms = ready?.decision_makers || []
  const namedCount = dms.filter(isNamedDecisionMaker).length
  const shipments = trade?.total_shipments || 0
  const suppliers = trade?.top_suppliers?.length || 0
  const reg = ready?.company_registry

  const tabs = useMemo(() => {
    if (!ready) return [] as Array<{ id: TabId; label: string }>
    const list: Array<{ id: TabId; label: string }> = [
      { id: 'overview', label: '经营画像' },
      { id: 'people', label: `决策人${dms.length ? ` ${dms.length}` : ''}` },
      {
        id: 'trade',
        label: `采购交易${shipments ? ` ${shipments}` : ''}`,
      },
      {
        id: 'supply',
        label: `供应链${suppliers ? ` ${suppliers}` : ''}`,
      },
      { id: 'entity', label: '主体资质' },
    ]
    return list
  }, [ready, dms.length, shipments, suppliers])

  const brand = (place.title || '')
    .replace(/\b(PT\.?|CV\.?|TBK\.?|Indonesia|Jakarta|Ltd\.?|Limited|Inc\.?|AG|GmbH)\b/gi, ' ')
    .replace(/\s+/g, ' ')
    .trim()
  const linkedInXray = `https://www.google.com/search?q=${encodeURIComponent(
    `site:linkedin.com/in "${brand || place.title || ''}" ("Purchasing Manager" OR Buyer OR Procurement OR Direktur OR Owner OR CEO)`,
  )}`

  const running =
    intel === 'loading' ||
    (intel &&
      intel !== 'loading' &&
      (intel.status === 'running' || intel.status === 'pending' || intel.note === '背调中') &&
      !intel.summary &&
      !intel.error)

  const legalName = reg?.name || place.title || '背调详情'
  const jurisdiction = reg?.jurisdiction || trade?.country || ''

  return (
    <aside
      className={cn(
        'fixed z-50 flex flex-col bg-white',
        'inset-x-0 bottom-0 max-h-[min(94vh,960px)] rounded-t-2xl border-t border-[#E5E7EB] shadow-[0_-8px_28px_rgba(15,23,42,0.12)]',
        // 外贸通客户详情是整页级宽栏；右侧大抽屉贴近该密度
        'md:inset-y-0 md:right-0 md:left-auto md:max-h-none md:w-[min(760px,58vw)] md:rounded-none md:border-l md:border-t-0 md:shadow-[-8px_0_28px_rgba(15,23,42,0.08)]',
      )}
      role="dialog"
      aria-label="客户背调"
    >
      <div className="mx-auto mt-2 h-1 w-10 rounded-full bg-[#E5E7EB] md:hidden" />

      {/* 外贸通式顶栏：公司名 + 状态徽章 */}
      <header className="shrink-0 border-b border-[#E5E7EB] bg-gradient-to-b from-[#F8FAFC] to-white px-5 py-4">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="text-[11px] font-semibold tracking-[0.08em] text-[#9CA3AF]">客户背调</div>
            <h2 className="mt-1 truncate text-xl font-semibold tracking-tight text-[#111827]">{legalName}</h2>
            <div className="mt-1.5 flex flex-wrap items-center gap-1.5 text-xs text-[#6B7280]">
              {place.address ? <span>{place.address}</span> : null}
              {jurisdiction ? (
                <span className="rounded-full bg-[#EEF2FF] px-2 py-0.5 font-medium text-[#2F6BFF]">{jurisdiction}</span>
              ) : null}
              {reg?.current_status ? (
                <span className="rounded-full bg-emerald-50 px-2 py-0.5 font-medium text-emerald-700">
                  {reg.current_status}
                </span>
              ) : null}
              {ready ? confBadge(ready.confidence) : null}
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            <Button
              size="icon-sm"
              variant="ghost"
              onClick={onRefresh}
              disabled={refreshing || intel === 'loading'}
              title="重新拉取背调"
            >
              {refreshing || intel === 'loading' ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <RefreshCw className="size-4" />
              )}
            </Button>
            <Button size="icon-sm" variant="ghost" onClick={onClose} title="关闭">
              <X className="size-4" />
            </Button>
          </div>
        </div>

        {/* KPI 条：贴近外贸通详情页顶部指标 */}
        {ready && !running ? (
          <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-4">
            <Metric label="采购提单" value={shipments ? String(shipments) : '—'} hint={trade?.date_end || trade?.source} />
            <Metric label="可触达决策人" value={String(namedCount || dms.length || 0)} hint={dms.length ? `${dms.length} 条线索` : undefined} />
            <Metric
              label="贸易伙伴"
              value={suppliers ? String(suppliers) : '—'}
              hint={trade?.role === 'supplier' ? '对美出口' : trade ? '美国进口' : undefined}
            />
            <Metric
              label="主体编号"
              value={reg?.company_number ? String(reg.company_number).slice(0, 10) : '—'}
              hint={reg?.incorporation_date || ready.domain}
            />
          </div>
        ) : null}
      </header>

      {running ? (
        <div className="border-b border-[#E5E7EB] px-5 py-2.5">
          <div className="h-1.5 overflow-hidden rounded-full bg-[#EEF2FF]">
            <div className="h-full w-1/3 animate-pulse rounded-full bg-[#2F6BFF]" />
          </div>
          <p className="mt-1.5 text-xs text-[#6B7280]">背调中，正在聚合海关 / 官网 / 决策人公开情报…</p>
        </div>
      ) : null}

      {ready && !running && tabs.length > 0 ? (
        <nav className="flex shrink-0 gap-0 overflow-x-auto border-b border-[#E5E7EB] px-2">
          {tabs.map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => setTab(t.id)}
              className={cn(
                'shrink-0 border-b-2 px-3.5 py-2.5 text-sm font-medium transition',
                tab === t.id
                  ? 'border-[#2F6BFF] text-[#2F6BFF]'
                  : 'border-transparent text-[#6B7280] hover:text-[#111827]',
              )}
            >
              {t.label}
            </button>
          ))}
        </nav>
      ) : null}

      <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
        {intel === 'loading' || !intel ? (
          <div className="flex items-center gap-2 py-8 text-sm text-[#6B7280]">
            <Loader2 className="size-4 animate-spin" /> 加载客户背调…
          </div>
        ) : intel.error && !intel.summary ? (
          <p className="text-sm text-red-600">暂时无法加载背调，请稍后重试。</p>
        ) : (
          <IntelTabBody tab={tab} intel={intel} place={place} linkedInXray={linkedInXray} />
        )}
      </div>
    </aside>
  )
}

function IntelTabBody({
  tab,
  intel,
  place,
  linkedInXray,
}: {
  tab: TabId
  intel: PlaceIntel
  place: PlaceLite
  linkedInXray: string
}) {
  const dms = intel.decision_makers || []
  const named = dms.filter(isNamedDecisionMaker)
  const channels = dms.filter((d) => !isNamedDecisionMaker(d))
  const emails = intel.extra_emails || []
  const dmEmails = new Set(dms.map((d) => (d.email || '').toLowerCase()).filter(Boolean))
  const personEmails = emails.filter((e) => !isGenericEmail(e))
  const officeEmails = emails.filter((e) => isGenericEmail(e) && !dmEmails.has(e.toLowerCase()))
  const phones = intel.phones || []
  const socials = intel.socials || {}
  const socialKeys = Object.keys(socials).filter((k) => socials[k])
  const orgs = (intel.org_structure || []).filter((o) => {
    if (!o?.name) return false
    const ev = `${o.evidence || ''} ${o.role || ''}`
    if (/google maps category|domain-registrant|rdap|whois|registrant/i.test(ev)) return false
    if (o.role === 'domain' || o.role === 'registrant') return false
    return true
  })
  const reg = intel.company_registry
  const trade = intel.trade
  const firm = intel.firmographics
  const tech = intel.technologies || []

  if (tab === 'overview') {
    return (
      <div className="space-y-4">
        <section className="rounded-xl border border-[#E5E7EB] bg-[#F8FAFC] p-3.5">
          <h4 className="text-xs font-semibold text-[#6B7280]">公司介绍</h4>
          <p className="mt-1.5 text-sm leading-relaxed text-[#374151]">
            {companyIntroText(intel) || '暂无公司介绍；可切换「采购交易 / 决策人」查看海关与联系人情报。'}
          </p>
        </section>
        <dl>
          <KV label="域名">{intel.domain || '—'}</KV>
          <KV label="网站">
            {intel.website || place.website ? (
              <ExtLink href={intel.website || place.website || ''}>
                {(intel.website || place.website || '').replace(/^https?:\/\//, '')}
              </ExtLink>
            ) : (
              '—'
            )}
          </KV>
          <KV label="企业邮 MX">{intel.mx_hosts?.length ? intel.mx_hosts.join(', ') : '—'}</KV>
          <KV label="触达信心">{confBadge(intel.confidence) || '—'}</KV>
        </dl>
        {firm && (firm.legal_name || firm.revenue_usd || firm.employees || firm.ticker) ? (
          <section>
            <h4 className="mb-2 text-xs font-semibold text-[#6B7280]">规模 / 财报（免费公开源）</h4>
            <dl>
              <KV label="法定名">{firm.legal_name || '—'}</KV>
              <KV label="Ticker">{firm.ticker || '—'}</KV>
              <KV label="营收">
                {firm.revenue_usd
                  ? `$${Number(firm.revenue_usd).toLocaleString()}${firm.revenue_year ? `（${firm.revenue_year}）` : ''}`
                  : '—'}
              </KV>
              <KV label="员工">{firm.employees ? String(firm.employees) : '—'}</KV>
              <KV label="来源">{firm.source || '—'}</KV>
              {firm.filing_url ? (
                <KV label="SEC">
                  <ExtLink href={firm.filing_url}>打开 10-K</ExtLink>
                </KV>
              ) : null}
            </dl>
          </section>
        ) : null}
        {tech.length ? (
          <section>
            <h4 className="mb-2 text-xs font-semibold text-[#6B7280]">建站 / 技术栈</h4>
            <div className="flex flex-wrap gap-1.5">
              {tech.map((t) => (
                <Chip key={t}>{t}</Chip>
              ))}
            </div>
          </section>
        ) : null}
        {socialKeys.length ? (
          <section>
            <h4 className="mb-2 text-xs font-semibold text-[#6B7280]">社媒活跃度</h4>
            <dl>
              {socialKeys.map((k) => (
                <KV key={k} label={k}>
                  <ExtLink href={socials[k]}>{socials[k].replace(/^https?:\/\//, '')}</ExtLink>
                </KV>
              ))}
            </dl>
          </section>
        ) : null}
        <p className="rounded-lg border border-dashed border-[#FECACA] bg-[#FEF2F2] px-3 py-2 text-xs leading-relaxed text-[#991B1B]">
          免费源已尽量用满：美国海运海关（Kirchner）、SEC 财报、Wikidata/官网决策人。全球海关全量 / 私企营收员工库 /
          合规 LinkedIn 批量仍需付费。
        </p>
      </div>
    )
  }

  if (tab === 'people') {
    return (
      <div className="space-y-3">
        <p className="text-sm leading-relaxed text-[#4B5563]">
          {intel.note || '分层展示老板 / 采购 / 运营等可核验决策人，并附邮箱、WhatsApp、LinkedIn。'}
        </p>
        <section>
          <h4 className="mb-2 text-xs font-semibold text-[#6B7280]">
            具名决策人{named.length ? `（${named.length}）` : ''}
          </h4>
          {named.length ? (
            <div className="space-y-2.5">{named.map((d, i) => <PersonCard key={`${d.name}-${i}`} d={d} />)}</div>
          ) : (
            <Empty text="暂无具名决策人；下方仅为触达渠道" />
          )}
        </section>
        {channels.length > 0 ? (
          <section>
            <h4 className="mb-2 text-xs font-semibold text-[#6B7280]">触达渠道（非决策人）</h4>
            <div className="flex flex-wrap gap-1.5">
              {channels.map((d, i) => {
                const parts = [d.title, d.email, d.phone, d.whatsapp].filter(Boolean)
                if (!parts.length && !d.linkedin) return null
                return (
                  <Chip key={i}>
                    {parts.join(' · ')}
                    {d.linkedin ? (
                      <>
                        {' · '}
                        <ExtLink href={d.linkedin}>LinkedIn</ExtLink>
                      </>
                    ) : null}
                  </Chip>
                )
              })}
            </div>
          </section>
        ) : null}
        {personEmails.filter((e) => !dmEmails.has(e.toLowerCase())).length > 0 ? (
          <section>
            <h4 className="mb-2 text-xs font-semibold text-[#6B7280]">疑似个人邮箱</h4>
            <div className="flex flex-wrap gap-1.5">
              {personEmails
                .filter((e) => !dmEmails.has(e.toLowerCase()))
                .map((e) => (
                  <Chip key={e}>
                    <a href={`mailto:${e}`} className="text-[#2F6BFF] hover:underline">
                      {e}
                    </a>
                  </Chip>
                ))}
            </div>
          </section>
        ) : null}
        <section>
          <h4 className="mb-2 text-xs font-semibold text-[#6B7280]">电话 / WhatsApp</h4>
          {phones.length ? (
            <div className="flex flex-wrap gap-1.5">
              {phones.slice(0, 8).map((p) => (
                <Chip key={p}>
                  {p}{' '}
                  <a
                    href={`https://wa.me/${p.replace(/\D/g, '')}`}
                    target="_blank"
                    rel="noreferrer"
                    className="text-emerald-700 hover:underline"
                  >
                    WA
                  </a>
                </Chip>
              ))}
            </div>
          ) : (
            <Empty text="暂无电话" />
          )}
        </section>
        {officeEmails.length > 0 ? (
          <section>
            <h4 className="mb-2 text-xs font-semibold text-[#6B7280]">总机 / 角色邮箱</h4>
            <div className="flex flex-wrap gap-1.5">
              {officeEmails.slice(0, 12).map((e) => (
                <Chip key={e}>{e}</Chip>
              ))}
            </div>
          </section>
        ) : null}
        <a
          href={linkedInXray}
          target="_blank"
          rel="noreferrer"
          className="inline-flex rounded-md bg-[#EEF2FF] px-3 py-2 text-xs font-semibold text-[#2F6BFF]"
        >
          用外贸公式搜 LinkedIn 决策人
        </a>
      </div>
    )
  }

  if (tab === 'trade') {
    if (
      !trade ||
      !(
        trade.total_shipments ||
        trade.top_hs_codes?.length ||
        trade.recent_bols?.length ||
        trade.top_suppliers?.length
      )
    ) {
      return <Empty text="暂无公开海关采购记录（外贸通级全球提单全量需商业海关库）" />
    }
    const months = [...(trade.monthly_shipments || [])].reverse()
    return (
      <div className="space-y-4">
        <p className="text-sm text-[#4B5563]">
          {trade.summary || '美国海运公开提单（Kirchner / ImportYeti 免费源）；下方为明细记录表。'}
        </p>
        <dl>
          <KV label="角色">{trade.role === 'supplier' ? '对美出口供应商' : '美国进口商'}</KV>
          <KV label="主体">{trade.name || '—'}</KV>
          <KV label="累计提单">{trade.total_shipments ? String(trade.total_shipments) : '—'}</KV>
          <KV label="去重供应商">{trade.unique_suppliers ? String(trade.unique_suppliers) : '—'}</KV>
          <KV label="去重品类">{trade.unique_products ? String(trade.unique_products) : '—'}</KV>
          <KV label="去年提单">{trade.last_year_total ? String(trade.last_year_total) : '—'}</KV>
          <KV label="最新月份">{trade.newest_month || '—'}</KV>
          <KV label="区间">{[trade.date_start, trade.date_end].filter(Boolean).join(' – ') || '—'}</KV>
          <KV label="主要原产国">{trade.country || '—'}</KV>
          <KV label="地址">{trade.address || '—'}</KV>
          {trade.phone ? <KV label="电话">{trade.phone}</KV> : null}
          {trade.profile_url ? (
            <KV label="档案">
              <ExtLink href={trade.profile_url}>打开</ExtLink>
            </KV>
          ) : null}
        </dl>

        {trade.recent_bols?.length ? (
          <DataTable
            title="近期提单明细"
            tall
            columns={[
              { key: 'date', label: '到港日' },
              { key: 'bol', label: '提单号 BOL' },
              { key: 'shipper', label: '托运人' },
              { key: 'consignee', label: '收货人' },
              { key: 'product', label: '品名 / 货描' },
              { key: 'vessel', label: '船名' },
              { key: 'carrier', label: '承运人' },
            ]}
            rows={trade.recent_bols.map((b) => ({
              date: b.date || '—',
              bol: <span className="font-semibold text-[#111827]">{b.bill_of_lading || '—'}</span>,
              shipper: b.shipper || '（未披露）',
              consignee: b.consignee || '—',
              product: b.product || (b.hs_code ? `HS ${b.hs_code}` : '—'),
              vessel: b.vessel || '—',
              carrier: b.carrier || '—',
            }))}
          />
        ) : (
          <p className="text-xs text-[#9CA3AF]">该免费源未返回逐票提单（仅聚合统计表）。</p>
        )}

        <DataTable
          title="核心 HS / 品类"
          columns={[
            { key: 'code', label: 'HS 编码' },
            { key: 'desc', label: '描述' },
            { key: 'n', label: '提单数', num: true },
          ]}
          rows={(trade.top_hs_codes || []).map((h) => ({
            code: <span className="font-semibold text-[#111827]">{h.code || '—'}</span>,
            desc: h.description || '—',
            n: h.shipments != null ? String(h.shipments) : '—',
          }))}
        />
        <DataTable
          title="品名词频"
          columns={[
            { key: 'term', label: '品名关键词' },
            { key: 'n', label: '出现次数', num: true },
          ]}
          rows={(trade.product_terms || []).map((h) => ({
            term: h.code || '—',
            n: h.shipments != null ? String(h.shipments) : '—',
          }))}
        />
        <DataTable
          title="原产国分布"
          columns={[
            { key: 'c', label: '原产国' },
            { key: 'n', label: '提单数', num: true },
          ]}
          rows={(trade.top_origins || []).map((o) => ({
            c: o.name || o.country || '—',
            n: o.shipments != null ? String(o.shipments) : '—',
          }))}
        />
        <DataTable
          title="承运人"
          columns={[
            { key: 'c', label: '承运人' },
            { key: 'n', label: '提单数', num: true },
          ]}
          rows={(trade.top_carriers || []).map((c) => ({
            c: c.name || '—',
            n: c.shipments != null ? String(c.shipments) : '—',
          }))}
        />
        <DataTable
          title="年度提单"
          columns={[
            { key: 'p', label: '年份' },
            { key: 'n', label: '提单数', num: true },
          ]}
          rows={(trade.yearly_shipments || []).map((y) => ({
            p: y.period || '—',
            n: String(y.shipments ?? 0),
          }))}
        />
        <DataTable
          title="逐月提单记录"
          tall
          columns={[
            { key: 'p', label: '月份' },
            { key: 'n', label: '提单数', num: true },
          ]}
          rows={months.map((y) => ({
            p: y.period || '—',
            n: String(y.shipments ?? 0),
          }))}
        />
        <DataTable
          title="主要航线"
          tall
          columns={[
            { key: 'o', label: '起运港' },
            { key: 'd', label: '目的港' },
            { key: 'n', label: '提单数', num: true },
          ]}
          rows={(trade.port_routes || []).map((r) => ({
            o: r.origin || '—',
            d: r.destination || '—',
            n: r.shipments != null ? String(r.shipments) : '—',
          }))}
        />
      </div>
    )
  }

  if (tab === 'supply') {
    const partners = (trade?.top_suppliers || []).filter((s) => {
      const n = (s.name || '').trim().toLowerCase()
      return n && n !== 'n/a' && n !== 'na'
    })
    const newest = trade?.newest_suppliers || []
    if (!partners.length && !newest.length && !trade?.growing_supplier?.name) {
      return <Empty text="暂无供应链伙伴公开记录（外贸通可做采供双向穿透）" />
    }
    return (
      <div className="space-y-4">
        <p className="text-sm text-[#4B5563]">现有供应商 / 贸易伙伴结构，用于判断切入窗口。</p>
        <DataTable
          title="主要供应商"
          tall
          columns={[
            { key: 'name', label: '供应商' },
            { key: 'country', label: '国家' },
            { key: 'n', label: '提单数', num: true },
            { key: 'link', label: '档案' },
          ]}
          rows={partners.map((s) => ({
            name: <span className="font-semibold text-[#111827]">{s.name}</span>,
            country: s.country || '—',
            n: s.shipments != null ? String(s.shipments) : '—',
            link: s.profile_url ? <ExtLink href={s.profile_url}>打开</ExtLink> : '—',
          }))}
        />
        <DataTable
          title="新晋供应商"
          columns={[
            { key: 'name', label: '供应商' },
            { key: 'first', label: '首票日期' },
            { key: 'n', label: '提单数', num: true },
          ]}
          rows={newest.map((s) => ({
            name: <span className="font-semibold text-[#111827]">{s.name}</span>,
            first: s.first_seen || '—',
            n: s.shipments != null ? String(s.shipments) : '—',
          }))}
        />
        {trade?.growing_supplier?.name ? (
          <DataTable
            title="增长最快供应商"
            columns={[
              { key: 'name', label: '供应商' },
              { key: 'n', label: '增长票数', num: true },
              { key: 'range', label: '区间' },
            ]}
            rows={[
              {
                name: <span className="font-semibold text-[#111827]">{trade.growing_supplier.name}</span>,
                n: trade.growing_supplier.shipments != null ? String(trade.growing_supplier.shipments) : '—',
                range: trade.growing_supplier.first_seen || '—',
              },
            ]}
          />
        ) : null}
      </div>
    )
  }

  if (tab === 'entity') {
    return (
      <div className="space-y-4">
        <section>
          <h4 className="mb-2 text-xs font-semibold text-[#6B7280]">工商 / 主体登记</h4>
          {reg?.name ? (
            <dl>
              <KV label="名称">{reg.name}</KV>
              <KV label="编号">{reg.company_number || '—'}</KV>
              <KV label="辖区">{reg.jurisdiction || '—'}</KV>
              <KV label="状态">{reg.current_status || '—'}</KV>
              <KV label="成立">{reg.incorporation_date || '—'}</KV>
              {reg.registry_url ? (
                <KV label="链接">
                  <ExtLink href={reg.registry_url}>打开登记页</ExtLink>
                </KV>
              ) : null}
            </dl>
          ) : (
            <Empty text="未匹配公开主体（全球工商全量需商业库）" />
          )}
        </section>
        <section>
          <h4 className="mb-2 text-xs font-semibold text-[#6B7280]">股权 / 架构</h4>
          {orgs.length ? (
            <ul className="space-y-2 text-sm text-[#374151]">
              {orgs.map((o, i) => (
                <li key={i} className="rounded-lg border border-[#E5E7EB] p-2.5">
                  <strong>{o.name}</strong>
                  {o.role ? ` · ${o.role}` : ''}
                  {o.parent ? <div className="text-xs text-[#9CA3AF]">上级：{o.parent}</div> : null}
                </li>
              ))}
            </ul>
          ) : (
            <Empty text="官网未公开组织架构（不会用域名注册人冒充）" />
          )}
        </section>
      </div>
    )
  }

  return null
}
