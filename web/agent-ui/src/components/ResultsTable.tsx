import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import { ChevronDown, Loader2, RefreshCw } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import type { JobMeta } from '@/lib/sessions'

type PlaceRow = {
  job_id: string
  place_id: string
  title: string
  category?: string
  address?: string
  phone?: string
  website?: string
  status?: string
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

async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const r = await fetch(url, { credentials: 'same-origin', ...init })
  const data = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error((data as { message?: string }).message || r.statusText)
  return data as T
}

function normalizePlace(p: Record<string, unknown>, jobId: string): PlaceRow {
  return {
    job_id: jobId,
    place_id: String(p.place_id || p.PlaceID || p.cid || p.Cid || ''),
    title: String(p.title || p.Title || '未命名'),
    category: (p.category || p.Category || p.categories || '') as string,
    address: (p.address || p.Address || '') as string,
    phone: (p.phone || p.Phone || p.phone_number || '') as string,
    website: (p.website || p.Website || '') as string,
  }
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
          const data = await fetchJSON<Record<string, unknown>[] | { places?: Record<string, unknown>[] }>(
            `/api/v1/jobs/${j.id}/places`,
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

      // Auto-start / refresh intel as soon as places appear (EnableIntel=true on agent jobs).
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
    <div className="mt-4 overflow-hidden rounded-2xl border border-[#E5E7EB] bg-white shadow-sm">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-[#E5E7EB] px-4 py-3">
        <div>
          <div className="text-sm font-semibold text-[#1F2937]">结果汇总</div>
          <div className="text-xs text-[#6B7280]">
            共 {rows.length} 家 · 结果出现后自动开始背调 · 点击行查看详情
            {pendingQueueHint ? ` · ${pendingQueueHint}` : ''}
          </div>
        </div>
        <Button size="sm" variant="outline" onClick={load} disabled={loading} className="rounded-full">
          {loading ? <Loader2 className="size-3.5 animate-spin" /> : <RefreshCw className="size-3.5" />}
          刷新
        </Button>
      </div>

      {/* Sub-task chips: click to filter growing results */}
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

      <div className="max-h-[480px] overflow-auto">
        <table className="w-full min-w-[780px] text-left text-sm">
          <thead className="sticky top-0 bg-[#F9FAFB] text-xs text-[#6B7280]">
            <tr>
              <th className="px-3 py-2.5 font-medium">商家</th>
              <th className="px-3 py-2.5 font-medium">品类</th>
              <th className="px-3 py-2.5 font-medium">电话</th>
              <th className="px-3 py-2.5 font-medium">地址</th>
              <th className="px-3 py-2.5 font-medium">背调</th>
              <th className="w-8 px-3 py-2.5" />
            </tr>
          </thead>
          <tbody>
            {visible.length === 0 && (
              <tr>
                <td colSpan={6} className="px-3 py-12 text-center text-[#9CA3AF]">
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
                    <td className="px-3 py-2.5 font-medium text-[#111827]">{row.title}</td>
                    <td className="px-3 py-2.5 text-[#6B7280]">{row.category || '—'}</td>
                    <td className="px-3 py-2.5">{row.phone || '—'}</td>
                    <td className="max-w-[240px] truncate px-3 py-2.5 text-[#6B7280]">
                      {row.address || '—'}
                    </td>
                    <td className="px-3 py-2.5">
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
                    </td>
                    <td className="px-3 py-2.5">
                      <ChevronDown
                        className={cn('size-4 text-[#9CA3AF] transition', open && 'rotate-180')}
                      />
                    </td>
                  </tr>
                  {open && (
                    <tr className="border-t border-[#F3F4F6] bg-[#F9FAFB]">
                      <td colSpan={6} className="px-4 py-3 text-sm">
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
