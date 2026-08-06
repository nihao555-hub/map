import { Fragment, useEffect, useState } from 'react'
import { ChevronDown, Loader2, RefreshCw } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

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
  decision_makers?: Array<{ name?: string; title?: string; email?: string; phone?: string }>
  org_structure?: Array<{ name?: string; role?: string }>
  error?: string
}

async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const r = await fetch(url, { credentials: 'same-origin', ...init })
  const data = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error((data as { message?: string }).message || r.statusText)
  return data as T
}

export function ResultsTable({ jobIds }: { jobIds: string[] }) {
  const [rows, setRows] = useState<PlaceRow[]>([])
  const [loading, setLoading] = useState(false)
  const [openId, setOpenId] = useState<string | null>(null)
  const [intel, setIntel] = useState<Record<string, IntelPayload | 'loading' | 'error'>>({})

  const load = async () => {
    if (!jobIds.length) return
    setLoading(true)
    try {
      const all: PlaceRow[] = []
      for (const id of jobIds) {
        try {
          const data = await fetchJSON<{ places?: PlaceRow[] } | PlaceRow[]>(
            `/api/v1/jobs/${id}/places?limit=200`,
          )
          const list = Array.isArray(data) ? data : data.places || []
          for (const p of list) {
            all.push({
              ...p,
              job_id: id,
              place_id: p.place_id || (p as { PlaceID?: string }).PlaceID || '',
              title: p.title || (p as { Title?: string }).Title || '未命名',
              category: p.category || (p as { Category?: string }).Category,
              address: p.address || (p as { Address?: string }).Address,
              phone: p.phone || (p as { Phone?: string }).Phone,
              website: p.website || (p as { Website?: string }).Website,
            })
          }
        } catch {
          /* job may still be pending */
        }
      }
      setRows(all)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    const t = setInterval(load, 8000)
    return () => clearInterval(t)
  }, [jobIds.join(',')])

  const toggleIntel = async (row: PlaceRow) => {
    const key = `${row.job_id}:${row.place_id}`
    if (openId === key) {
      setOpenId(null)
      return
    }
    setOpenId(key)
    if (intel[key] && intel[key] !== 'error') return
    setIntel((m) => ({ ...m, [key]: 'loading' }))
    try {
      // POST triggers / refreshes intel; GET reads. Prefer GET then POST if empty.
      let data = await fetchJSON<IntelPayload>(
        `/api/v1/jobs/${row.job_id}/places/${encodeURIComponent(row.place_id)}/intel`,
      )
      if (!data.summary && data.status !== 'ok') {
        data = await fetchJSON<IntelPayload>(
          `/api/v1/jobs/${row.job_id}/places/${encodeURIComponent(row.place_id)}/intel`,
          { method: 'POST' },
        )
      }
      setIntel((m) => ({ ...m, [key]: data }))
    } catch (e) {
      setIntel((m) => ({
        ...m,
        [key]: { error: e instanceof Error ? e.message : '背调失败' },
      }))
    }
  }

  if (!jobIds.length) return null

  return (
    <div className="mt-4 overflow-hidden rounded-2xl border border-border bg-card shadow-sm">
      <div className="flex items-center justify-between border-b border-border px-4 py-3">
        <div>
          <div className="text-sm font-semibold">结果汇总</div>
          <div className="text-xs text-muted-foreground">
            已抓 {rows.length} 家 · 点击行展开背调（结果出来后才可背调）
          </div>
        </div>
        <Button size="sm" variant="outline" onClick={load} disabled={loading}>
          {loading ? <Loader2 className="size-3.5 animate-spin" /> : <RefreshCw className="size-3.5" />}
          刷新
        </Button>
      </div>
      <div className="max-h-[420px] overflow-auto">
        <table className="w-full min-w-[720px] text-left text-sm">
          <thead className="sticky top-0 bg-muted/80 text-xs text-muted-foreground backdrop-blur">
            <tr>
              <th className="px-3 py-2 font-medium">商家</th>
              <th className="px-3 py-2 font-medium">品类</th>
              <th className="px-3 py-2 font-medium">电话</th>
              <th className="px-3 py-2 font-medium">地址</th>
              <th className="px-3 py-2 font-medium w-10" />
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr>
                <td colSpan={5} className="px-3 py-10 text-center text-muted-foreground">
                  {loading ? '正在拉取结果…' : '任务排队/抓取中，稍后自动出现结果'}
                </td>
              </tr>
            )}
            {rows.map((row) => {
              const key = `${row.job_id}:${row.place_id}`
              const open = openId === key
              const intelState = intel[key]
              return (
                <Fragment key={key}>
                  <tr
                    className={cn(
                      'cursor-pointer border-t border-border/70 hover:bg-accent/40',
                      open && 'bg-accent/30',
                    )}
                    onClick={() => toggleIntel(row)}
                  >
                    <td className="px-3 py-2.5 font-medium">{row.title}</td>
                    <td className="px-3 py-2.5 text-muted-foreground">{row.category || '—'}</td>
                    <td className="px-3 py-2.5">{row.phone || '—'}</td>
                    <td className="max-w-[240px] truncate px-3 py-2.5 text-muted-foreground">
                      {row.address || '—'}
                    </td>
                    <td className="px-3 py-2.5">
                      <ChevronDown
                        className={cn('size-4 text-muted-foreground transition', open && 'rotate-180')}
                      />
                    </td>
                  </tr>
                  {open && (
                    <tr key={key + '-intel'} className="border-t border-border/50 bg-muted/30">
                      <td colSpan={5} className="px-4 py-3">
                        {intelState === 'loading' && (
                          <div className="flex items-center gap-2 text-sm text-muted-foreground">
                            <Loader2 className="size-4 animate-spin" /> 加载背调…
                          </div>
                        )}
                        {typeof intelState === 'object' && intelState !== null && (
                          <div className="space-y-2 text-sm">
                            {'error' in intelState && (intelState as IntelPayload).error ? (
                              <p className="text-destructive">{intelState.error}</p>
                            ) : (
                              <>
                                <p className="leading-relaxed">
                                  {(intelState as IntelPayload).summary || '暂无背调摘要（结果出来后自动可跑）'}
                                </p>
                                {!!(intelState as IntelPayload).decision_makers?.length && (
                                  <div className="text-xs text-muted-foreground">
                                    决策人：
                                    {(intelState as IntelPayload).decision_makers!
                                      .map((d) => [d.name, d.title, d.email, d.phone].filter(Boolean).join(' / '))
                                      .join('；')}
                                  </div>
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
