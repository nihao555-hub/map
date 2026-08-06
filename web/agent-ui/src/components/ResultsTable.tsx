import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import { ChevronDown, Loader2, RefreshCw } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import type { JobMeta } from '@/lib/sessions'

/** Full Place payload from GET /api/v1/jobs/{id}/places?full=1 */
type PlaceRow = {
  job_id: string
  place_id: string
  title: string
  category?: string
  address?: string
  complete_address?: string
  phone?: string
  website?: string
  emails?: string
  whatsapp?: string
  facebook?: string
  instagram?: string
  linkedin?: string
  twitter?: string
  tiktok?: string
  youtube?: string
  telegram?: string
  pinterest?: string
  status?: string
  open_hours?: string
  popular_times?: string
  price_range?: string
  descriptions?: string
  about?: string
  menu?: string
  owner?: string
  review_rating?: number
  review_count?: number
  reviews_per_rating?: string
  reviews_link?: string
  user_reviews?: string
  latitude?: number
  longitude?: number
  plus_code?: string
  timezone?: string
  link?: string
  thumbnail?: string
  images?: string
  cid?: string
  data_id?: string
  credit_cards_accepted?: string
  reservations?: string
  order_online?: string
  street_view_url?: string
}

type IntelPayload = {
  summary?: string
  status?: string
  note?: string
  decision_makers?: Array<{ name?: string; title?: string; email?: string; phone?: string }>
  error?: string
}

type QueueInfo = {
  id: string
  status: string
  ahead: number
  pending_total?: number
  message: string
}

const COLS = [
  { key: 'title', label: '名称', min: 160 },
  { key: 'category', label: '类别', min: 120 },
  { key: 'phone', label: '电话', min: 120 },
  { key: 'emails', label: '邮箱', min: 160 },
  { key: 'whatsapp', label: 'WhatsApp', min: 110 },
  { key: 'website', label: '网站', min: 160 },
  { key: 'address', label: '地址', min: 220 },
  { key: 'complete_address', label: '完整地址', min: 200 },
  { key: 'review_rating', label: '评分', min: 64 },
  { key: 'review_count', label: '评论数', min: 72 },
  { key: 'status', label: '状态', min: 80 },
  { key: 'open_hours', label: '营业时间', min: 140 },
  { key: 'price_range', label: '价格区间', min: 80 },
  { key: 'owner', label: '店主', min: 120 },
  { key: 'descriptions', label: '描述', min: 180 },
  { key: 'about', label: 'About', min: 160 },
  { key: 'latitude', label: '纬度', min: 90 },
  { key: 'longitude', label: '经度', min: 90 },
  { key: 'plus_code', label: 'Plus Code', min: 110 },
  { key: 'timezone', label: '时区', min: 100 },
  { key: 'facebook', label: 'Facebook', min: 120 },
  { key: 'instagram', label: 'Instagram', min: 120 },
  { key: 'linkedin', label: 'LinkedIn', min: 120 },
  { key: 'twitter', label: 'Twitter', min: 100 },
  { key: 'tiktok', label: 'TikTok', min: 100 },
  { key: 'youtube', label: 'YouTube', min: 100 },
  { key: 'telegram', label: 'Telegram', min: 100 },
  { key: 'pinterest', label: 'Pinterest', min: 100 },
  { key: 'cid', label: 'CID', min: 100 },
  { key: 'place_id', label: 'Place ID', min: 120 },
  { key: 'link', label: 'Maps', min: 72 },
  { key: 'intel', label: '背调', min: 80 },
] as const

const COLSPAN = COLS.length + 1 // + chevron

async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const r = await fetch(url, { credentials: 'same-origin', ...init })
  const data = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error((data as { message?: string }).message || r.statusText)
  return data as T
}

function str(v: unknown): string {
  if (v === undefined || v === null) return ''
  return String(v).trim()
}

function truncate(s: string, n: number): string {
  if (!s) return ''
  return s.length > n ? `${s.slice(0, n)}…` : s
}

function normalizePlace(p: Record<string, unknown>, jobId: string): PlaceRow {
  const placeId = str(p.place_id || p.PlaceID || p.cid || p.Cid)
  return {
    job_id: jobId,
    place_id: placeId,
    title: str(p.title || p.Title) || '未命名',
    category: str(p.category || p.Category),
    address: str(p.address || p.Address),
    complete_address: str(p.complete_address || p.CompleteAddress),
    phone: str(p.phone || p.Phone),
    website: str(p.website || p.Website),
    emails: str(p.emails || p.Emails),
    whatsapp: str(p.whatsapp || p.WhatsApp),
    facebook: str(p.facebook || p.Facebook),
    instagram: str(p.instagram || p.Instagram),
    linkedin: str(p.linkedin || p.LinkedIn),
    twitter: str(p.twitter || p.Twitter),
    tiktok: str(p.tiktok || p.TikTok),
    youtube: str(p.youtube || p.YouTube),
    telegram: str(p.telegram || p.Telegram),
    pinterest: str(p.pinterest || p.Pinterest),
    status: str(p.status || p.Status),
    open_hours: str(p.open_hours || p.OpenHours),
    popular_times: str(p.popular_times || p.PopularTimes),
    price_range: str(p.price_range || p.PriceRange),
    descriptions: str(p.descriptions || p.Descriptions),
    about: str(p.about || p.About),
    menu: str(p.menu || p.Menu),
    owner: str(p.owner || p.Owner),
    review_rating: typeof p.review_rating === 'number' ? p.review_rating : Number(p.review_rating) || undefined,
    review_count: typeof p.review_count === 'number' ? p.review_count : Number(p.review_count) || undefined,
    reviews_per_rating: str(p.reviews_per_rating),
    reviews_link: str(p.reviews_link),
    user_reviews: str(p.user_reviews),
    latitude: typeof p.latitude === 'number' ? p.latitude : Number(p.latitude) || undefined,
    longitude: typeof p.longitude === 'number' ? p.longitude : Number(p.longitude) || undefined,
    plus_code: str(p.plus_code),
    timezone: str(p.timezone),
    link: str(p.link || p.Link),
    thumbnail: str(p.thumbnail),
    images: str(p.images),
    cid: str(p.cid || p.Cid),
    data_id: str(p.data_id),
    credit_cards_accepted: str(p.credit_cards_accepted),
    reservations: str(p.reservations),
    order_online: str(p.order_online),
    street_view_url: str(p.street_view_url),
  }
}

function cellValue(row: PlaceRow, key: string): string {
  switch (key) {
    case 'title':
      return row.title
    case 'review_rating':
      return row.review_rating != null ? row.review_rating.toFixed(1) : ''
    case 'review_count':
      return row.review_count != null ? String(row.review_count) : ''
    case 'latitude':
      return row.latitude != null ? row.latitude.toFixed(5) : ''
    case 'longitude':
      return row.longitude != null ? row.longitude.toFixed(5) : ''
    case 'link':
      return row.link || ''
    case 'intel':
      return ''
    default: {
      const v = (row as Record<string, unknown>)[key]
      return typeof v === 'string' || typeof v === 'number' ? String(v) : ''
    }
  }
}

function CellContent({ row, colKey }: { row: PlaceRow; colKey: string }) {
  const raw = cellValue(row, colKey)
  if (colKey === 'link' && raw) {
    return (
      <a href={raw} target="_blank" rel="noreferrer" className="text-[#2F6BFF] hover:underline" onClick={(e) => e.stopPropagation()}>
        打开
      </a>
    )
  }
  if (colKey === 'website' && raw) {
    const href = raw.startsWith('http') ? raw : `https://${raw}`
    return (
      <a
        href={href}
        target="_blank"
        rel="noreferrer"
        className="text-[#2F6BFF] hover:underline"
        title={raw}
        onClick={(e) => e.stopPropagation()}
      >
        {truncate(raw.replace(/^https?:\/\//, ''), 36)}
      </a>
    )
  }
  if (['facebook', 'instagram', 'linkedin', 'twitter', 'tiktok', 'youtube', 'telegram', 'pinterest', 'whatsapp'].includes(colKey) && raw) {
    const href = raw.startsWith('http') || raw.startsWith('wa.me') || raw.startsWith('+')
      ? raw.startsWith('+')
        ? `https://wa.me/${raw.replace(/\D/g, '')}`
        : raw.startsWith('http')
          ? raw
          : `https://${raw}`
      : raw
    if (href.startsWith('http')) {
      return (
        <a href={href} target="_blank" rel="noreferrer" className="text-[#2F6BFF] hover:underline" title={raw} onClick={(e) => e.stopPropagation()}>
          {truncate(raw.replace(/^https?:\/\//, ''), 28)}
        </a>
      )
    }
  }
  if (!raw) return <span className="text-[#9CA3AF]">—</span>
  const max = ['descriptions', 'about', 'open_hours', 'complete_address', 'address', 'emails'].includes(colKey) ? 80 : 48
  return <span title={raw}>{truncate(raw, max)}</span>
}

export function ResultsTable({ jobs }: { jobs: JobMeta[] }) {
  const [rows, setRows] = useState<PlaceRow[]>([])
  const [counts, setCounts] = useState<Record<string, number>>({})
  const [queue, setQueue] = useState<Record<string, QueueInfo>>({})
  const [activeJob, setActiveJob] = useState<string>('all')
  const [loading, setLoading] = useState(false)
  const [openId, setOpenId] = useState<string | null>(null)
  const [intel, setIntel] = useState<Record<string, IntelPayload | 'loading'>>({})
  const autoIntelStarted = useRef<Set<string>>(new Set())
  const intelDone = useRef<Set<string>>(new Set())

  const loadQueue = async () => {
    if (!jobs.length) return
    try {
      const data = await fetchJSON<{ jobs?: QueueInfo[] }>('/api/v1/agent/jobs/queue', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ job_ids: jobs.map((j) => j.id) }),
      })
      const next: Record<string, QueueInfo> = {}
      for (const q of data.jobs || []) {
        if (q.id) next[q.id] = q
      }
      setQueue(next)
    } catch {
      /* ignore */
    }
  }

  const load = async () => {
    if (!jobs.length) return
    setLoading(true)
    try {
      await loadQueue()
      const all: PlaceRow[] = []
      const nextCounts: Record<string, number> = {}
      for (const j of jobs) {
        try {
          const countRes = await fetchJSON<{ count: number }>(`/api/v1/jobs/${j.id}/places/count`)
          nextCounts[j.id] = countRes.count || 0
        } catch {
          nextCounts[j.id] = 0
        }
        try {
          // full=1: all CSV lead fields (emails, socials, coords, owner, …)
          const data = await fetchJSON<Record<string, unknown>[] | { places?: Record<string, unknown>[] }>(
            `/api/v1/jobs/${j.id}/places?full=1`,
          )
          const list = Array.isArray(data) ? data : data.places || []
          for (const p of list) {
            const row = normalizePlace(p, j.id)
            if (row.place_id) all.push(row)
          }
        } catch {
          /* pending */
        }
      }
      setCounts(nextCounts)
      setRows(all)

      for (const row of all) {
        const key = `${row.job_id}:${row.place_id}`
        if (intelDone.current.has(key)) continue
        autoIntelStarted.current.add(key)
        fetchJSON<IntelPayload>(
          `/api/v1/jobs/${row.job_id}/places/${encodeURIComponent(row.place_id)}/intel`,
        )
          .then((data) => {
            const finished =
              !!data.summary && data.status !== 'running' && data.note !== '背调中' && !data.error
            if (finished) intelDone.current.add(key)
            setIntel((m) => ({ ...m, [key]: data }))
          })
          .catch(() => {})
      }
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    const t = setInterval(load, 5000)
    return () => clearInterval(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [jobs.map((j) => j.id).join(',')])

  const pendingQueueHint = useMemo(() => {
    const pending = jobs
      .map((j) => queue[j.id])
      .filter((q): q is QueueInfo => !!q && q.status === 'pending')
    if (!pending.length) return ''
    const minAhead = Math.min(...pending.map((q) => q.ahead))
    if (minAhead <= 0) return '部分子任务排队中，即将开始'
    return `部分子任务排队中，前面还有 ${minAhead} 个任务`
  }, [jobs, queue])

  const visible = useMemo(
    () => (activeJob === 'all' ? rows : rows.filter((r) => r.job_id === activeJob)),
    [rows, activeJob],
  )

  const tableMinWidth = useMemo(() => COLS.reduce((s, c) => s + c.min, 40), [])

  const toggleRow = async (row: PlaceRow) => {
    const key = `${row.job_id}:${row.place_id}`
    if (openId === key) {
      setOpenId(null)
      return
    }
    setOpenId(key)
    if (intel[key] && intel[key] !== 'loading') return
    setIntel((m) => ({ ...m, [key]: 'loading' }))
    try {
      const data = await fetchJSON<IntelPayload>(
        `/api/v1/jobs/${row.job_id}/places/${encodeURIComponent(row.place_id)}/intel`,
      )
      setIntel((m) => ({ ...m, [key]: data }))
    } catch (e) {
      setIntel((m) => ({
        ...m,
        [key]: { error: e instanceof Error ? e.message : '背调失败' },
      }))
    }
  }

  if (!jobs.length) return null

  return (
    <div className="mt-4 w-full min-w-0 overflow-hidden rounded-2xl border border-[#E5E7EB] bg-white shadow-sm">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-[#E5E7EB] px-4 py-3">
        <div>
          <div className="text-sm font-semibold text-[#1F2937]">结果汇总</div>
          <div className="text-xs text-[#6B7280]">
            共 {rows.length} 家 · 全量字段 · 列多时可左右滑动 · 结果出现后自动背调 · 点击行查看详情
            {pendingQueueHint ? ` · ${pendingQueueHint}` : ''}
          </div>
        </div>
        <Button size="sm" variant="outline" onClick={load} disabled={loading} className="rounded-full">
          {loading ? <Loader2 className="size-3.5 animate-spin" /> : <RefreshCw className="size-3.5" />}
          刷新
        </Button>
      </div>

      <div className="flex gap-2 overflow-x-auto border-b border-[#E5E7EB] px-3 py-2">
        <button
          type="button"
          onClick={() => setActiveJob('all')}
          className={cn(
            'shrink-0 rounded-full px-3 py-1.5 text-xs font-medium',
            activeJob === 'all' ? 'bg-[#2F6BFF] text-white' : 'bg-[#F3F4F6] text-[#4B5563]',
          )}
        >
          全部 ({rows.length})
        </button>
        {jobs.map((j) => {
          const q = queue[j.id]
          const pending = q?.status === 'pending'
          const chipLabel = pending
            ? q.ahead > 0
              ? `${j.name} · 前方 ${q.ahead}`
              : `${j.name} · 排队中`
            : q?.status === 'working'
              ? `${j.name} · 抓取中 (${counts[j.id] || 0})`
              : `${j.name} (${counts[j.id] || 0})`
          return (
            <button
              key={j.id}
              type="button"
              onClick={() => setActiveJob(j.id)}
              className={cn(
                'max-w-[260px] shrink-0 truncate rounded-full px-3 py-1.5 text-xs font-medium',
                activeJob === j.id ? 'bg-[#2F6BFF] text-white' : 'bg-[#F3F4F6] text-[#4B5563]',
              )}
              title={pending ? q.message || j.name : j.name}
            >
              {chipLabel}
            </button>
          )
        })}
      </div>

      {/* Wide full-field table: horizontal + vertical scroll */}
      <div className="max-h-[520px] w-full overflow-auto">
        <table className="w-max text-left text-sm" style={{ minWidth: tableMinWidth }}>
          <thead className="sticky top-0 z-10 bg-[#F9FAFB] text-xs text-[#6B7280]">
            <tr>
              {COLS.map((c) => (
                <th
                  key={c.key}
                  className="whitespace-nowrap px-3 py-2.5 font-medium"
                  style={{ minWidth: c.min }}
                >
                  {c.label}
                </th>
              ))}
              <th className="w-8 px-3 py-2.5" />
            </tr>
          </thead>
          <tbody>
            {visible.length === 0 && (
              <tr>
                <td colSpan={COLSPAN} className="px-3 py-12 text-center text-[#9CA3AF]">
                  {loading
                    ? '正在拉取结果…'
                    : (() => {
                        const focus =
                          activeJob === 'all'
                            ? jobs.map((j) => queue[j.id]).find((q) => q?.status === 'pending')
                            : queue[activeJob]
                        if (focus?.status === 'pending') {
                          return focus.message || '排队中，等待执行'
                        }
                        return '子任务抓取中，结果会持续增加'
                      })()}
                </td>
              </tr>
            )}
            {visible.map((row) => {
              const key = `${row.job_id}:${row.place_id}`
              const open = openId === key
              const st = intel[key]
              const intelLabel =
                !st || st === 'loading'
                  ? st === 'loading'
                    ? '加载中'
                    : autoIntelStarted.current.has(key)
                      ? '背调中'
                      : '等待'
                  : st.status === 'running' || st.note === '背调中'
                    ? '背调中'
                    : st.status === 'skipped'
                      ? '已跳过'
                      : st.summary
                        ? '已完成'
                        : st.error
                          ? '失败'
                          : '背调中'
              return (
                <Fragment key={key}>
                  <tr
                    className={cn(
                      'cursor-pointer border-t border-[#F3F4F6] hover:bg-[#F5F8FF]',
                      open && 'bg-[#F5F8FF]',
                    )}
                    onClick={() => toggleRow(row)}
                  >
                    {COLS.map((c) => (
                      <td
                        key={c.key}
                        className={cn(
                          'px-3 py-2.5 align-top',
                          c.key === 'title' && 'font-medium text-[#111827]',
                          ['category', 'address', 'complete_address', 'descriptions', 'about'].includes(c.key) &&
                            'text-[#6B7280]',
                          'max-w-[280px]',
                        )}
                      >
                        {c.key === 'intel' ? (
                          <span
                            className={cn(
                              'rounded-full px-2 py-0.5 text-[11px]',
                              intelLabel === '已完成'
                                ? 'bg-emerald-50 text-emerald-700'
                                : intelLabel === '失败'
                                  ? 'bg-red-50 text-red-600'
                                  : 'bg-blue-50 text-[#2F6BFF]',
                            )}
                          >
                            {intelLabel}
                          </span>
                        ) : (
                          <CellContent row={row} colKey={c.key} />
                        )}
                      </td>
                    ))}
                    <td className="px-3 py-2.5">
                      <ChevronDown
                        className={cn('size-4 text-[#9CA3AF] transition', open && 'rotate-180')}
                      />
                    </td>
                  </tr>
                  {open && (
                    <tr className="border-t border-[#F3F4F6] bg-[#F9FAFB]">
                      <td colSpan={COLSPAN} className="px-4 py-3 text-sm">
                        {st === 'loading' && (
                          <div className="flex items-center gap-2 text-[#6B7280]">
                            <Loader2 className="size-4 animate-spin" /> 加载背调…
                          </div>
                        )}
                        {st && st !== 'loading' && (
                          <div className="space-y-2">
                            {st.error ? (
                              <p className="text-red-600">{st.error}</p>
                            ) : (
                              <>
                                <p className="leading-relaxed text-[#374151]">
                                  {st.summary || st.note || '背调进行中，稍后自动更新'}
                                </p>
                                {!!st.decision_makers?.length && (
                                  <p className="text-xs text-[#6B7280]">
                                    决策人：
                                    {st.decision_makers
                                      .map((d) =>
                                        [d.name, d.title, d.email, d.phone].filter(Boolean).join(' / '),
                                      )
                                      .join('；')}
                                  </p>
                                )}
                              </>
                            )}
                            {(row.about || row.descriptions || row.open_hours) && (
                              <div className="mt-2 grid gap-1 border-t border-[#E5E7EB] pt-2 text-xs text-[#6B7280]">
                                {row.open_hours ? <p>营业：{truncate(row.open_hours, 240)}</p> : null}
                                {row.descriptions ? <p>描述：{truncate(row.descriptions, 320)}</p> : null}
                                {row.about ? <p>About：{truncate(row.about, 320)}</p> : null}
                              </div>
                            )}
                          </div>
                        )}
                      </td>
                    </tr>
                  )}
                </Fragment>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}
