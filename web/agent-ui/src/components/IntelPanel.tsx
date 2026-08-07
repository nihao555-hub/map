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

type TabId = 'people' | 'overview' | 'customs' | 'org' | 'domain'

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
        'ml-1 rounded-full px-1.5 py-0.5 text-[10px] font-medium',
        c === 'high' && 'bg-emerald-50 text-emerald-700',
        c === 'medium' && 'bg-amber-50 text-amber-700',
        c === 'low' && 'bg-slate-100 text-slate-600',
      )}
    >
      信心 {label}
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
    <div className="grid grid-cols-[72px_1fr] gap-2 py-1 text-sm">
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
  return <p className="py-2 text-sm text-[#9CA3AF]">{text}</p>
}

function PersonCard({ d }: { d: DecisionMaker }) {
  const name = (d.name || '').trim()
  if (!name) return null
  const wa = d.whatsapp || d.phone
  const waHref = wa ? `https://wa.me/${String(wa).replace(/\D/g, '')}` : ''
  return (
    <article className="rounded-xl border border-[#E5E7EB] bg-white p-3">
      <div className="flex gap-3">
        {d.avatar ? (
          <img
            src={d.avatar}
            alt=""
            className="size-10 shrink-0 rounded-full object-cover"
            loading="lazy"
            referrerPolicy="no-referrer"
            onError={(e) => {
              ;(e.target as HTMLImageElement).style.display = 'none'
            }}
          />
        ) : (
          <div className="flex size-10 shrink-0 items-center justify-center rounded-full bg-[#EEF2FF] text-sm font-semibold text-[#2F6BFF]">
            {name.charAt(0).toUpperCase()}
          </div>
        )}
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-1">
            <strong className="text-sm text-[#111827]">{name}</strong>
            {confBadge(d.confidence)}
          </div>
          {d.title ? <div className="text-xs text-[#6B7280]">{d.title}</div> : null}
          {d.location ? <div className="text-xs text-[#9CA3AF]">{d.location}</div> : null}
        </div>
      </div>
      <dl className="mt-2 space-y-0.5">
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
      <div className="mt-2 flex flex-wrap gap-1.5">
        {d.email ? (
          <a
            href={`mailto:${d.email}`}
            className="rounded-md bg-[#EEF2FF] px-2 py-1 text-[11px] font-medium text-[#2F6BFF]"
          >
            邮件
          </a>
        ) : null}
        {waHref ? (
          <a
            href={waHref}
            target="_blank"
            rel="noreferrer"
            className="rounded-md bg-emerald-50 px-2 py-1 text-[11px] font-medium text-emerald-700"
          >
            WhatsApp
          </a>
        ) : null}
        {d.linkedin ? (
          <a
            href={d.linkedin.startsWith('http') ? d.linkedin : `https://${d.linkedin}`}
            target="_blank"
            rel="noreferrer"
            className="rounded-md bg-[#EEF2FF] px-2 py-1 text-[11px] font-medium text-[#2F6BFF]"
          >
            LinkedIn
          </a>
        ) : null}
      </div>
      {d.evidence ? <p className="mt-2 text-[11px] text-[#9CA3AF]">证据：{d.evidence}</p> : null}
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
  const [tab, setTab] = useState<TabId>('people')

  const tabs = useMemo(() => {
    if (!intel || intel === 'loading') return [] as Array<{ id: TabId; label: string }>
    const dms = intel.decision_makers || []
    const trade = intel.trade
    const hasTrade =
      !!trade &&
      !!(trade.total_shipments || trade.top_suppliers?.length || trade.top_hs_codes?.length)
    const orgs = (intel.org_structure || []).filter((o) => {
      if (!o?.name) return false
      const ev = `${o.evidence || ''} ${o.role || ''}`
      if (/google maps category|domain-registrant|rdap|whois|registrant/i.test(ev)) return false
      if (o.role === 'domain' || o.role === 'registrant') return false
      return true
    })
    const list: Array<{ id: TabId; label: string }> = [
      { id: 'people', label: `决策人${dms.length ? ` (${dms.length})` : ''}` },
      { id: 'overview', label: '公司' },
    ]
    if (hasTrade) {
      list.push({
        id: 'customs',
        label: `海关${trade?.total_shipments ? ` (${trade.total_shipments})` : ''}`,
      })
    }
    list.push(
      { id: 'org', label: `架构${orgs.length ? ` (${orgs.length})` : ''}` },
      { id: 'domain', label: '主体' },
    )
    return list
  }, [intel])

  const brand = (place.title || '')
    .replace(/\b(PT\.?|CV\.?|TBK\.?|Indonesia|Jakarta|Ltd\.?|Limited)\b/gi, ' ')
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

  return (
    <aside
      className={cn(
        'fixed z-50 flex flex-col bg-white',
        // Phone: bottom sheet. Tablet/desktop: right drawer.
        'inset-x-0 bottom-0 max-h-[min(92vh,920px)] rounded-t-2xl border-t border-[#E5E7EB] shadow-[0_-8px_28px_rgba(15,23,42,0.12)]',
        'md:inset-y-0 md:right-0 md:left-auto md:max-h-none md:w-[min(420px,100vw)] md:rounded-none md:border-l md:border-t-0 md:shadow-[-8px_0_28px_rgba(15,23,42,0.08)]',
      )}
      role="dialog"
      aria-label="背调详情"
    >
      <div className="mx-auto mt-2 h-1 w-10 rounded-full bg-[#E5E7EB] md:hidden" />
      <header className="flex shrink-0 items-start justify-between gap-2 border-b border-[#E5E7EB] px-4 py-3">
        <div className="min-w-0">
          <div className="text-[11px] font-medium tracking-wide text-[#9CA3AF]">商户背调</div>
          <h2 className="truncate text-base font-semibold text-[#111827]">{place.title || '背调详情'}</h2>
          {place.address ? <p className="mt-0.5 truncate text-xs text-[#6B7280]">{place.address}</p> : null}
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
      </header>

      {running ? (
        <div className="border-b border-[#E5E7EB] px-4 py-2">
          <div className="h-1.5 overflow-hidden rounded-full bg-[#EEF2FF]">
            <div className="h-full w-1/3 animate-pulse rounded-full bg-[#2F6BFF]" />
          </div>
          <p className="mt-1.5 text-xs text-[#6B7280]">背调中，正在拉取公开情报…</p>
        </div>
      ) : null}

      {intel && intel !== 'loading' && !running && tabs.length > 0 ? (
        <nav className="flex shrink-0 gap-1 overflow-x-auto border-b border-[#E5E7EB] px-3 py-2">
          {tabs.map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => setTab(t.id)}
              className={cn(
                'shrink-0 rounded-full px-2.5 py-1 text-xs font-medium transition',
                tab === t.id ? 'bg-[#2F6BFF] text-white' : 'bg-[#F3F4F6] text-[#4B5563] hover:bg-[#E5E7EB]',
              )}
            >
              {t.label}
            </button>
          ))}
        </nav>
      ) : null}

      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
        {intel === 'loading' || !intel ? (
          <div className="flex items-center gap-2 py-8 text-sm text-[#6B7280]">
            <Loader2 className="size-4 animate-spin" /> 加载背调…
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
  const blob = `${place.title || ''} ${place.address || ''}`.toLowerCase()
  const localOffice = officeEmails.filter((e) => {
    const em = e.toLowerCase()
    return (
      (blob.includes('indonesia') && em.includes('indonesia')) ||
      (blob.includes('jakarta') && em.includes('indonesia')) ||
      /^info@/.test(em)
    )
  })
  const otherOffice = officeEmails.filter((e) => !localOffice.includes(e))
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

  if (tab === 'people') {
    return (
      <div className="space-y-3">
        <p className="text-sm leading-relaxed text-[#4B5563]">
          {intel.note || '优先展示可核验决策人及其邮箱/WhatsApp/LinkedIn。'}
        </p>
        <section>
          <h4 className="mb-2 text-xs font-semibold text-[#6B7280]">
            具名决策人{named.length ? `（${named.length}）` : ''}
          </h4>
          {named.length ? (
            <div className="space-y-2">{named.map((d, i) => <PersonCard key={`${d.name}-${i}`} d={d} />)}</div>
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
              {phones.slice(0, 6).map((p) => (
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
        {localOffice.length > 0 ? (
          <section>
            <h4 className="mb-2 text-xs font-semibold text-[#6B7280]">本地/总机邮箱</h4>
            <div className="flex flex-wrap gap-1.5">
              {localOffice.slice(0, 5).map((e) => (
                <Chip key={e}>{e}</Chip>
              ))}
            </div>
          </section>
        ) : null}
        {otherOffice.length > 0 ? (
          <details className="text-sm">
            <summary className="cursor-pointer text-xs font-semibold text-[#6B7280]">
              全球办公室邮箱（{otherOffice.length}）
            </summary>
            <div className="mt-2 flex flex-wrap gap-1.5">
              {otherOffice.slice(0, 20).map((e) => (
                <Chip key={e}>{e}</Chip>
              ))}
            </div>
          </details>
        ) : null}
        {socialKeys.length > 0 ? (
          <section>
            <h4 className="mb-2 text-xs font-semibold text-[#6B7280]">社媒</h4>
            <dl>
              {socialKeys.map((k) => (
                <KV key={k} label={k}>
                  <ExtLink href={socials[k]}>{socials[k].replace(/^https?:\/\//, '')}</ExtLink>
                </KV>
              ))}
            </dl>
          </section>
        ) : null}
        <a
          href={linkedInXray}
          target="_blank"
          rel="noreferrer"
          className="inline-flex rounded-md bg-[#EEF2FF] px-3 py-2 text-xs font-medium text-[#2F6BFF]"
        >
          用外贸公式搜 LinkedIn 决策人
        </a>
      </div>
    )
  }

  if (tab === 'overview') {
    return (
      <dl className="space-y-0.5">
        <KV label="触达信心">{confBadge(intel.confidence) || '—'}</KV>
        <KV label="摘要">{intel.summary || '—'}</KV>
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
      </dl>
    )
  }

  if (tab === 'customs' && trade) {
    return (
      <div className="space-y-3">
        <p className="text-sm text-[#4B5563]">
          {trade.summary || '美国海关海运提单记录（ImportYeti / Kirchner）'}
        </p>
        <dl>
          <KV label="角色">{trade.role === 'supplier' ? '对美出口供应商' : '美国进口商'}</KV>
          <KV label="主体">{trade.name || '—'}</KV>
          <KV label="提单数">{trade.total_shipments ? String(trade.total_shipments) : '—'}</KV>
          <KV label="区间">
            {[trade.date_start, trade.date_end].filter(Boolean).join(' – ') || '—'}
          </KV>
          <KV label="国家">{trade.country || '—'}</KV>
          <KV label="地址">{trade.address || '—'}</KV>
          {trade.profile_url ? (
            <KV label="档案">
              <ExtLink href={trade.profile_url}>打开</ExtLink>
            </KV>
          ) : null}
        </dl>
        {trade.top_suppliers?.length ? (
          <section>
            <h4 className="mb-2 text-xs font-semibold text-[#6B7280]">主要贸易伙伴</h4>
            <ul className="space-y-1.5 text-sm text-[#374151]">
              {trade.top_suppliers.map((s, i) => (
                <li key={i}>
                  <strong>{s.name}</strong>
                  {s.country ? ` · ${s.country}` : ''}
                  {s.shipments ? ` · ${s.shipments}票` : ''}
                </li>
              ))}
            </ul>
          </section>
        ) : null}
        {trade.top_hs_codes?.length ? (
          <section>
            <h4 className="mb-2 text-xs font-semibold text-[#6B7280]">HS 编码</h4>
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
            <ul className="space-y-1.5 text-sm text-[#374151]">
              {trade.recent_bols.map((b, i) => (
                <li key={i}>
                  {b.date || ''} · {b.shipper || ''} → {b.consignee || ''}
                  {b.hs_code ? ` · HS ${b.hs_code}` : ''}
                  {b.product ? <div className="text-xs text-[#9CA3AF]">{b.product}</div> : null}
                </li>
              ))}
            </ul>
          </section>
        ) : null}
      </div>
    )
  }

  if (tab === 'org') {
    return orgs.length ? (
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
      <Empty text="官网未公开组织架构（不会用域名注册人冒充架构）" />
    )
  }

  if (tab === 'domain') {
    return (
      <div className="space-y-3">
        <h4 className="text-xs font-semibold text-[#6B7280]">主体登记</h4>
        {reg?.name ? (
          <dl>
            <KV label="名称">{reg.name}</KV>
            <KV label="编号">{reg.company_number || '—'}</KV>
            <KV label="辖区">{reg.jurisdiction || '—'}</KV>
            <KV label="状态">{reg.current_status || '—'}</KV>
            <KV label="成立">{reg.incorporation_date || '—'}</KV>
            {reg.registry_url ? (
              <KV label="链接">
                <ExtLink href={reg.registry_url}>打开</ExtLink>
              </KV>
            ) : null}
          </dl>
        ) : (
          <Empty text="未匹配公开主体" />
        )}
      </div>
    )
  }

  return null
}
