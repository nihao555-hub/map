import { useMemo, useState } from 'react'
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
    date_start?: string
    date_end?: string
    top_suppliers?: Array<{ name?: string; country?: string; shipments?: number; profile_url?: string }>
    top_hs_codes?: Array<{ code?: string; description?: string; shipments?: number }>
    recent_bols?: Array<{
      date?: string
      shipper?: string
      consignee?: string
      product?: string
      hs_code?: string
    }>
    summary?: string
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
  const tech = intel.technologies || []

  if (tab === 'overview') {
    return (
      <div className="space-y-4">
        <section className="rounded-xl border border-[#E5E7EB] bg-[#F8FAFC] p-3.5">
          <h4 className="text-xs font-semibold text-[#6B7280]">经营摘要</h4>
          <p className="mt-1.5 text-sm leading-relaxed text-[#374151]">
            {intel.summary || intel.note || '暂无摘要；可切换「采购交易 / 决策人」查看已挖到的公开情报。'}
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
          与外贸通差距：全球海关全量库、营收/员工商业库、合规 LinkedIn 批量档案需付费数据；本页为官网 +
          公开海关片段 + 决策人穿透的可落地替代。
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
    if (!trade || !(trade.total_shipments || trade.top_hs_codes?.length || trade.recent_bols?.length)) {
      return (
        <Empty text="暂无公开海关采购记录（外贸通级全球提单全量需商业海关库）" />
      )
    }
    return (
      <div className="space-y-3">
        <p className="text-sm text-[#4B5563]">
          {trade.summary || '公开海关海运提单片段（免费源）；用于判断是否真实买家 / 采购品类。'}
        </p>
        <dl>
          <KV label="角色">{trade.role === 'supplier' ? '对美出口供应商' : '美国进口商'}</KV>
          <KV label="主体">{trade.name || '—'}</KV>
          <KV label="提单数">{trade.total_shipments ? String(trade.total_shipments) : '—'}</KV>
          <KV label="区间">{[trade.date_start, trade.date_end].filter(Boolean).join(' – ') || '—'}</KV>
          <KV label="国家">{trade.country || '—'}</KV>
          <KV label="地址">{trade.address || '—'}</KV>
          {trade.profile_url ? (
            <KV label="档案">
              <ExtLink href={trade.profile_url}>打开</ExtLink>
            </KV>
          ) : null}
        </dl>
        {trade.top_hs_codes?.length ? (
          <section>
            <h4 className="mb-2 text-xs font-semibold text-[#6B7280]">核心 HS / 品类</h4>
            <div className="flex flex-wrap gap-1.5">
              {trade.top_hs_codes.map((h, i) => (
                <Chip key={i}>
                  {h.code}
                  {h.description ? ` ${h.description}` : ''}
                  {h.shipments ? ` (${h.shipments})` : ''}
                </Chip>
              ))}
            </div>
          </section>
        ) : null}
        {trade.recent_bols?.length ? (
          <section>
            <h4 className="mb-2 text-xs font-semibold text-[#6B7280]">近期提单</h4>
            <ul className="space-y-2 text-sm text-[#374151]">
              {trade.recent_bols.map((b, i) => (
                <li key={i} className="rounded-lg border border-[#E5E7EB] p-2.5">
                  <div className="font-medium">
                    {b.date || ''} · {b.shipper || ''} → {b.consignee || ''}
                  </div>
                  {b.hs_code ? <div className="text-xs text-[#6B7280]">HS {b.hs_code}</div> : null}
                  {b.product ? <div className="text-xs text-[#9CA3AF]">{b.product}</div> : null}
                </li>
              ))}
            </ul>
          </section>
        ) : null}
      </div>
    )
  }

  if (tab === 'supply') {
    const partners = trade?.top_suppliers || []
    if (!partners.length) {
      return <Empty text="暂无供应链伙伴公开记录（外贸通可做采供双向穿透）" />
    }
    return (
      <div className="space-y-3">
        <p className="text-sm text-[#4B5563]">现有供应商 / 贸易伙伴结构，用于判断切入窗口。</p>
        <ul className="space-y-2">
          {partners.map((s, i) => (
            <li key={i} className="rounded-xl border border-[#E5E7EB] p-3 text-sm text-[#374151]">
              <strong className="text-[#111827]">{s.name}</strong>
              {s.country ? <span className="text-[#6B7280]"> · {s.country}</span> : null}
              {s.shipments ? <span className="text-[#6B7280]"> · {s.shipments} 票</span> : null}
              {s.profile_url ? (
                <div className="mt-1">
                  <ExtLink href={s.profile_url}>查看档案</ExtLink>
                </div>
              ) : null}
            </li>
          ))}
        </ul>
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
