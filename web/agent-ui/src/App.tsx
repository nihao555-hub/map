import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Bot,
  Compass,
  Loader2,
  MapPin,
  Menu,
  MessageSquarePlus,
  PanelLeftClose,
  Rocket,
  Sparkles,
} from 'lucide-react'
import {
  Conversation,
  ConversationContent,
  ConversationEmptyState,
  ConversationScrollButton,
} from '@/components/ai-elements/conversation'
import { Message, MessageContent, MessageResponse } from '@/components/ai-elements/message'
import {
  ChainOfThought,
  ChainOfThoughtContent,
  ChainOfThoughtHeader,
  ChainOfThoughtSearchResult,
  ChainOfThoughtSearchResults,
  ChainOfThoughtStep,
} from '@/components/ai-elements/chain-of-thought'
import { Task, TaskContent, TaskItem, TaskItemFile, TaskTrigger } from '@/components/ai-elements/task'
import { Suggestion, Suggestions } from '@/components/ai-elements/suggestion'
import { PromptInput } from '@/components/ai-elements/prompt-input'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  loadSessions,
  newSession,
  saveSessions,
  titleFromGoal,
  type AgentPlan,
  type AgentSession,
  type ChatMessage,
} from '@/lib/sessions'
import { cn } from '@/lib/utils'

const SUGGESTIONS = [
  '覆盖整个雅加达找咖啡馆和进口商',
  '在曼谷找美容店，半径 15 公里并做背调',
  'Find importers in Surabaya within 20km',
  '在吉隆坡找咖啡馆，尽量找全',
]

type StepStatus = 'complete' | 'active' | 'pending'

type PipelineState = {
  intent: StepStatus
  plan: StepStatus
  localize: StepStatus
  dispatch: StepStatus
  scrape: StepStatus
}

const idlePipeline = (): PipelineState => ({
  intent: 'pending',
  plan: 'pending',
  localize: 'pending',
  dispatch: 'pending',
  scrape: 'pending',
})

async function apiJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const r = await fetch(url, {
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', ...(init?.headers || {}) },
    ...init,
  })
  const data = await r.json().catch(() => ({}))
  if (!r.ok) {
    throw new Error((data && (data.message || data.error)) || r.statusText)
  }
  return data as T
}

function sleep(ms: number) {
  return new Promise((r) => setTimeout(r, ms))
}

export default function App() {
  const [sessions, setSessions] = useState<AgentSession[]>(() => {
    const list = loadSessions()
    return list.length ? list : [newSession()]
  })
  const [activeId, setActiveId] = useState(() => sessions[0]?.id)
  const [sidebarOpen, setSidebarOpen] = useState(true)
  const [draft, setDraft] = useState('')
  const [busy, setBusy] = useState(false)
  const [pipeline, setPipeline] = useState<PipelineState>(idlePipeline)
  const [aiStatus, setAiStatus] = useState<{ enabled: boolean; model: string } | null>(null)
  const [conc, setConc] = useState<string>('')

  const active = useMemo(
    () => sessions.find((s) => s.id === activeId) || sessions[0],
    [sessions, activeId],
  )

  useEffect(() => {
    saveSessions(sessions)
  }, [sessions])

  useEffect(() => {
    apiJSON<{ enabled: boolean; model: string }>('/api/v1/ai-status')
      .then(setAiStatus)
      .catch(() => setAiStatus({ enabled: false, model: '—' }))
    const tick = () =>
      apiJSON<{ active_jobs: number; admit_slots: number; per_job_deep_workers: number }>(
        '/api/v1/system/concurrency',
      )
        .then((s) =>
          setConc(`并发 ${s.active_jobs}/${s.admit_slots} · 满速×${s.per_job_deep_workers}`),
        )
        .catch(() => {})
    tick()
    const id = setInterval(tick, 5000)
    return () => clearInterval(id)
  }, [])

  const patchActive = useCallback(
    (fn: (s: AgentSession) => AgentSession) => {
      setSessions((prev) =>
        prev.map((s) => (s.id === activeId ? { ...fn(s), updatedAt: Date.now() } : s)),
      )
    },
    [activeId],
  )

  const createTask = () => {
    const s = newSession()
    setSessions((prev) => [s, ...prev])
    setActiveId(s.id)
    setDraft('')
    setPipeline(idlePipeline())
  }

  const runGoal = async (goal: string, dispatchAfter = true) => {
    if (!goal.trim() || busy) return
    setBusy(true)
    setPipeline({
      intent: 'active',
      plan: 'pending',
      localize: 'pending',
      dispatch: 'pending',
      scrape: 'pending',
    })

    const userMsg: ChatMessage = {
      id: crypto.randomUUID(),
      role: 'user',
      text: goal.trim(),
      createdAt: Date.now(),
    }
    patchActive((s) => ({
      ...s,
      title: s.messages.length === 0 ? titleFromGoal(goal) : s.title,
      messages: [...s.messages, userMsg],
    }))
    setDraft('')

    try {
      await sleep(280)
      const plan = await apiJSON<AgentPlan>('/api/v1/agent/understand', {
        method: 'POST',
        body: JSON.stringify({ goal: goal.trim(), ui_lang: 'zh' }),
      })
      setPipeline((p) => ({ ...p, intent: 'complete', plan: 'active' }))
      await sleep(220)
      setPipeline((p) => ({ ...p, plan: 'complete', localize: 'active' }))
      await sleep(180)
      setPipeline((p) => ({ ...p, localize: 'complete' }))

      let jobIds: string[] | undefined
      if (dispatchAfter) {
        setPipeline((p) => ({ ...p, dispatch: 'active' }))
        const dispatched = await apiJSON<{ plan: AgentPlan; job_ids: string[]; message: string }>(
          '/api/v1/agent/dispatch',
          {
            method: 'POST',
            body: JSON.stringify({ goal: goal.trim(), ui_lang: 'zh', intent: plan.intent }),
          },
        )
        jobIds = dispatched.job_ids || []
        setPipeline((p) => ({ ...p, dispatch: 'complete', scrape: 'active' }))
      }

      const n = plan.tasks?.length || 0
      const src = plan.intent?.source === 'ai' ? '真实 AI' : '规则引擎'
      const assistant: ChatMessage = {
        id: crypto.randomUUID(),
        role: 'assistant',
        text: dispatchAfter
          ? `已理解并分发 ${n} 个深度全量子任务（${src}）。公平准入下满速并行，其余排队。`
          : `已规划 ${n} 个深度全量子任务（${src}）。确认后可一键开始抓取。`,
        plan,
        jobIds,
        createdAt: Date.now(),
      }
      patchActive((s) => ({ ...s, messages: [...s.messages, assistant] }))
    } catch (e) {
      setPipeline(idlePipeline())
      const assistant: ChatMessage = {
        id: crypto.randomUUID(),
        role: 'assistant',
        text: '处理失败',
        error: e instanceof Error ? e.message : String(e),
        createdAt: Date.now(),
      }
      patchActive((s) => ({ ...s, messages: [...s.messages, assistant] }))
    } finally {
      setBusy(false)
    }
  }

  const dispatchOnly = async (plan: AgentPlan) => {
    if (busy) return
    setBusy(true)
    setPipeline((p) => ({ ...p, dispatch: 'active' }))
    try {
      const dispatched = await apiJSON<{ job_ids: string[]; message: string }>(
        '/api/v1/agent/dispatch',
        {
          method: 'POST',
          body: JSON.stringify({
            goal: plan.intent.raw_goal,
            ui_lang: 'zh',
            intent: plan.intent,
          }),
        },
      )
      setPipeline((p) => ({ ...p, dispatch: 'complete', scrape: 'active' }))
      const assistant: ChatMessage = {
        id: crypto.randomUUID(),
        role: 'assistant',
        text: dispatched.message || `已创建 ${(dispatched.job_ids || []).length} 个任务`,
        jobIds: dispatched.job_ids,
        createdAt: Date.now(),
      }
      patchActive((s) => ({ ...s, messages: [...s.messages, assistant] }))
    } catch (e) {
      const assistant: ChatMessage = {
        id: crypto.randomUUID(),
        role: 'assistant',
        text: '分发失败',
        error: e instanceof Error ? e.message : String(e),
        createdAt: Date.now(),
      }
      patchActive((s) => ({ ...s, messages: [...s.messages, assistant] }))
    } finally {
      setBusy(false)
    }
  }

  const empty = !active?.messages.length

  return (
    <div className="flex h-full min-h-0">
      {/* Sidebar — Doubao-like task rail */}
      <aside
        className={cn(
          'flex h-full shrink-0 flex-col border-r border-[var(--color-border)] bg-[var(--color-card)]/80 backdrop-blur transition-all',
          sidebarOpen ? 'w-[272px]' : 'w-0 overflow-hidden border-0',
        )}
      >
        <div className="flex items-center gap-2 px-4 pt-4 pb-3">
          <div className="flex size-8 items-center justify-center rounded-xl bg-[var(--color-primary)] text-[var(--color-primary-foreground)]">
            <Bot className="size-4" />
          </div>
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-semibold">地图获客 Agent</div>
            <div className="truncate text-[11px] text-[var(--color-muted-foreground)]">
              深度全量 · 目标找全
            </div>
          </div>
        </div>

        <div className="px-3 pb-3">
          <Button className="w-full justify-start gap-2" onClick={createTask}>
            <MessageSquarePlus className="size-4" />
            新建任务
          </Button>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-4">
          <p className="px-2 pb-2 text-[11px] font-medium tracking-wide text-[var(--color-muted-foreground)] uppercase">
            最近任务
          </p>
          <div className="space-y-1">
            {sessions.map((s) => (
              <button
                key={s.id}
                type="button"
                onClick={() => {
                  setActiveId(s.id)
                  setPipeline(idlePipeline())
                }}
                className={cn(
                  'flex w-full flex-col rounded-xl px-3 py-2.5 text-left transition-colors',
                  s.id === activeId
                    ? 'bg-[var(--color-accent)] text-[var(--color-accent-foreground)]'
                    : 'hover:bg-[var(--color-muted)]',
                )}
              >
                <span className="truncate text-sm font-medium">{s.title}</span>
                <span className="truncate text-[11px] opacity-70">
                  {new Date(s.updatedAt).toLocaleString()}
                </span>
              </button>
            ))}
          </div>
        </div>

        <div className="border-t border-[var(--color-border)] p-3 text-[11px] text-[var(--color-muted-foreground)]">
          <a href="/" className="inline-flex items-center gap-1 hover:text-[var(--color-foreground)]">
            <Compass className="size-3.5" />
            返回普通地图模式
          </a>
        </div>
      </aside>

      {/* Main chat */}
      <main className="flex min-w-0 flex-1 flex-col">
        <header className="flex items-center gap-3 border-b border-[var(--color-border)] bg-[var(--color-card)]/70 px-4 py-3 backdrop-blur">
          <Button
            size="icon-sm"
            variant="ghost"
            onClick={() => setSidebarOpen((v) => !v)}
            aria-label="切换侧栏"
          >
            {sidebarOpen ? <PanelLeftClose className="size-4" /> : <Menu className="size-4" />}
          </Button>
          <div className="min-w-0 flex-1">
            <div className="truncate text-sm font-semibold">{active?.title || '新任务'}</div>
            <div className="truncate text-[11px] text-[var(--color-muted-foreground)]">
              {aiStatus
                ? aiStatus.enabled
                  ? `AI · ${aiStatus.model}`
                  : `AI 未配置 · 规则引擎 · 默认 ${aiStatus.model}`
                : 'AI …'}
              {conc ? ` · ${conc}` : ''}
            </div>
          </div>
          <Badge variant="secondary">智能体工作区</Badge>
        </header>

        <Conversation className="min-h-0">
          <ConversationContent className="mx-auto w-full max-w-3xl">
            {empty ? (
              <ConversationEmptyState
                className="min-h-[52vh]"
                icon={<Sparkles className="size-10 text-[var(--color-primary)]" />}
                title="用一句话描述获客目标"
                description="Agent 会理解地点与品类，拆成深度全量子任务，尽量把目标地点符合的全部找出来"
              >
                <div className="mt-2 w-full max-w-xl space-y-5">
                  <div className="text-center">
                    <h2 className="text-2xl font-semibold tracking-tight">地图获客 Agent</h2>
                    <p className="mt-2 text-sm text-[var(--color-muted-foreground)]">
                      新建任务 → 描述目标 → 查看推理与计划 → 一键全量抓取
                    </p>
                  </div>
                  <Suggestions>
                    {SUGGESTIONS.map((s) => (
                      <Suggestion key={s} suggestion={s} onClick={(v) => runGoal(v, true)} />
                    ))}
                  </Suggestions>
                </div>
              </ConversationEmptyState>
            ) : (
              active?.messages.map((m) => (
                <Message key={m.id} from={m.role}>
                  <MessageContent>
                    <MessageResponse>{m.text}</MessageResponse>
                    {m.error && (
                      <p className="mt-2 text-sm text-[var(--color-destructive)]">{m.error}</p>
                    )}
                    {m.role === 'assistant' && m.plan && (
                      <div className="mt-4 space-y-4">
                        <ChainOfThought defaultOpen>
                          <ChainOfThoughtHeader>Agent 推理过程</ChainOfThoughtHeader>
                          <ChainOfThoughtContent>
                            <ChainOfThoughtStep
                              icon={Bot}
                              label="IntentAgent · 理解目标"
                              description={`${m.plan.intent.location || '—'} · ${m.plan.intent.country_name || m.plan.intent.country_code} · ${m.plan.intent.radius_km}km · source=${m.plan.intent.source}`}
                              status={pipeline.intent === 'pending' ? 'complete' : pipeline.intent}
                            >
                              <ChainOfThoughtSearchResults>
                                {(m.plan.intent.keywords || []).map((k) => (
                                  <ChainOfThoughtSearchResult key={k}>{k}</ChainOfThoughtSearchResult>
                                ))}
                              </ChainOfThoughtSearchResults>
                            </ChainOfThoughtStep>
                            <ChainOfThoughtStep
                              icon={MapPin}
                              label="PlannerAgent · 拆分子任务"
                              description={
                                m.plan.intent.notes ||
                                `共 ${m.plan.tasks.length} 个深度全量任务（多区县重叠覆盖）`
                              }
                              status="complete"
                            />
                            <ChainOfThoughtStep
                              icon={Sparkles}
                              label="LocalizerAgent · 本地化关键词"
                              description="译成 Maps 可搜词并锚定坐标"
                              status="complete"
                            />
                            <ChainOfThoughtStep
                              icon={Rocket}
                              label="DispatcherAgent · 分发抓取"
                              description={
                                m.jobIds?.length
                                  ? `已创建 ${m.jobIds.length} 个任务`
                                  : '等待确认分发'
                              }
                              status={m.jobIds?.length ? 'complete' : 'pending'}
                            />
                          </ChainOfThoughtContent>
                        </ChainOfThought>

                        <Task defaultOpen>
                          <TaskTrigger title={`抓取计划 · ${m.plan.tasks.length} 项`} />
                          <TaskContent>
                            {m.plan.tasks.map((t, i) => (
                              <TaskItem key={`${t.name}-${i}`}>
                                <div className="flex flex-wrap items-center gap-2">
                                  <span className="font-medium text-[var(--color-foreground)]">
                                    {t.name}
                                  </span>
                                  <TaskItemFile>{t.radius_km}km</TaskItemFile>
                                  <TaskItemFile>深度全量</TaskItemFile>
                                  {t.enable_intel && <TaskItemFile>背调</TaskItemFile>}
                                </div>
                                <div className="mt-1 text-xs">
                                  {(t.keywords || []).join(', ')} · {t.location}
                                </div>
                              </TaskItem>
                            ))}
                          </TaskContent>
                        </Task>

                        {!m.jobIds?.length && (
                          <Button
                            disabled={busy}
                            onClick={() => dispatchOnly(m.plan!)}
                            className="gap-2"
                          >
                            {busy ? (
                              <Loader2 className="size-4 animate-spin" />
                            ) : (
                              <Rocket className="size-4" />
                            )}
                            开始全量抓取
                          </Button>
                        )}

                        {!!m.jobIds?.length && (
                          <div className="rounded-xl border border-[var(--color-border)] bg-[var(--color-muted)]/50 p-3 text-xs">
                            <div className="mb-1 font-medium">已分发任务 ID</div>
                            <div className="flex flex-wrap gap-1.5">
                              {m.jobIds.map((id) => (
                                <a
                                  key={id}
                                  href={`/?focus=${id}`}
                                  className="rounded-md bg-[var(--color-card)] px-2 py-1 font-mono text-[11px] underline-offset-2 hover:underline"
                                >
                                  {id.slice(0, 8)}…
                                </a>
                              ))}
                            </div>
                            <a
                              href="/"
                              className="mt-2 inline-flex text-[var(--color-primary)] hover:underline"
                            >
                              在地图模式查看进度 →
                            </a>
                          </div>
                        )}
                      </div>
                    )}
                  </MessageContent>
                </Message>
              ))
            )}

            {busy && (
              <Message from="assistant">
                <MessageContent>
                  <div className="flex items-center gap-2 text-sm text-[var(--color-muted-foreground)]">
                    <Loader2 className="size-4 animate-spin text-[var(--color-primary)]" />
                    Agent 正在理解与规划…
                  </div>
                  <div className="mt-3">
                    <ChainOfThought defaultOpen>
                      <ChainOfThoughtHeader>实时步骤</ChainOfThoughtHeader>
                      <ChainOfThoughtContent>
                        {(
                          [
                            ['intent', 'IntentAgent'],
                            ['plan', 'PlannerAgent'],
                            ['localize', 'LocalizerAgent'],
                            ['dispatch', 'DispatcherAgent'],
                            ['scrape', 'Scraper'],
                          ] as const
                        ).map(([key, label]) => (
                          <ChainOfThoughtStep
                            key={key}
                            label={label}
                            status={pipeline[key]}
                            description={
                              pipeline[key] === 'active'
                                ? '进行中'
                                : pipeline[key] === 'complete'
                                  ? '完成'
                                  : '等待'
                            }
                          />
                        ))}
                      </ChainOfThoughtContent>
                    </ChainOfThought>
                  </div>
                </MessageContent>
              </Message>
            )}
          </ConversationContent>
          <ConversationScrollButton />
        </Conversation>

        <div className="border-t border-[var(--color-border)] bg-[var(--color-card)]/80 px-4 py-3 backdrop-blur">
          <div className="mx-auto w-full max-w-3xl">
            <PromptInput
              value={draft}
              onValueChange={setDraft}
              busy={busy}
              onSubmit={(text) => runGoal(text, true)}
              placeholder="例如：覆盖整个雅加达找咖啡馆和进口商，并做背调"
            />
          </div>
        </div>
      </main>
    </div>
  )
}
