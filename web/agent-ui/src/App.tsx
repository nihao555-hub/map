import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  ArrowRight,
  Bell,
  Menu,
  MessageSquarePlus,
  PanelLeftClose,
  RefreshCw,
  UserRound,
} from 'lucide-react'
import {
  Conversation,
  ConversationContent,
  ConversationScrollButton,
} from '@/components/ai-elements/conversation'
import { Message, MessageContent, MessageResponse } from '@/components/ai-elements/message'
import {
  ChainOfThought,
  ChainOfThoughtContent,
  ChainOfThoughtHeader,
  ChainOfThoughtStep,
} from '@/components/ai-elements/chain-of-thought'
import {
  Reasoning,
  ReasoningContent,
  ReasoningTrigger,
} from '@/components/ai-elements/reasoning'
import { Task, TaskContent, TaskItem, TaskItemFile, TaskTrigger } from '@/components/ai-elements/task'
import { Suggestion, Suggestions } from '@/components/ai-elements/suggestion'
import {
  PromptInput,
  PromptInputBody,
  PromptInputFooter,
  PromptInputSubmit,
  PromptInputTextarea,
} from '@/components/ai-elements/prompt-input'
import { Button } from '@/components/ui/button'
import { TooltipProvider } from '@/components/ui/tooltip'
import { ResultsTable } from '@/components/ResultsTable'
import {
  loadSessions,
  newSession,
  saveSessions,
  titleFromGoal,
  type AgentSession,
  type ChatMessage,
  type PipelineStep,
} from '@/lib/sessions'
import { cn } from '@/lib/utils'

const SUGGESTIONS = [
  '帮我找雅加达的咖啡馆，尽量找全',
  '覆盖整个雅加达找进口商和咖啡馆',
  '在曼谷找美容店，半径15公里并做背调',
  'Find importers in Surabaya within 20km',
  '在吉隆坡找咖啡馆',
  '在胡志明市找餐饮店，半径10公里',
]

const QUICK_START = [
  {
    title: '智能搜索线索',
    desc: '一句话描述地点与品类，自动深度全量抓取',
    icon: '/agent/decor/qs-search.png',
    prompt: '帮我找雅加达的咖啡馆，尽量找全',
  },
  {
    title: '地图区域获客',
    desc: '回地图模式圈选区域后继续搜索',
    icon: '/agent/decor/qs-map.png',
    href: '/',
  },
  {
    title: '结果汇总表',
    desc: '抓取完成后在此汇总查看与导出',
    icon: '/agent/decor/qs-chart.png',
    prompt: '覆盖整个雅加达找咖啡馆和进口商',
  },
  {
    title: '商家背调',
    desc: '结果出来后点击表格行展开背调',
    icon: '/agent/decor/qs-shop.png',
    prompt: '在雅加达找咖啡馆并做背调',
  },
]

async function apiJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const r = await fetch(url, {
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', ...(init?.headers || {}) },
    ...init,
  })
  const data = await r.json().catch(() => ({}))
  if (!r.ok) throw new Error((data as { message?: string }).message || r.statusText)
  return data as T
}

export default function App() {
  const [sessions, setSessions] = useState<AgentSession[]>(() => {
    const list = loadSessions()
    return list.length ? list : [newSession()]
  })
  const [activeId, setActiveId] = useState(() => sessions[0]?.id)
  const [sidebarOpen, setSidebarOpen] = useState(true)
  const [busy, setBusy] = useState(false)
  const [liveSteps, setLiveSteps] = useState<PipelineStep[]>([])
  const [aiMeta, setAiMeta] = useState<{ enabled: boolean; model: string } | null>(null)
  const [suggestions, setSuggestions] = useState(SUGGESTIONS)

  const active = useMemo(
    () => sessions.find((s) => s.id === activeId) || sessions[0],
    [sessions, activeId],
  )

  useEffect(() => {
    saveSessions(sessions)
  }, [sessions])

  useEffect(() => {
    apiJSON<{ enabled: boolean; model: string }>('/api/v1/ai-status')
      .then(setAiMeta)
      .catch(() => setAiMeta({ enabled: false, model: 'gemini-3.1-flash-lite' }))
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
    setLiveSteps([])
  }

  /** Single standard pipeline: NL → Intent→Plan→Localize→Dispatch→Scrape (intel later). */
  const runGoal = async (goal: string) => {
    if (!goal.trim() || busy) return
    setBusy(true)
    setLiveSteps([
      { id: 'intent', role: 'IntentAgent', title: '理解目标', status: 'active', summary: '解析自然语言…' },
      { id: 'plan', role: 'PlannerAgent', title: '规划全量任务', status: 'pending', summary: '等待' },
      { id: 'localize', role: 'LocalizerAgent', title: '本地化与锚定', status: 'pending', summary: '等待' },
      { id: 'dispatch', role: 'DispatcherAgent', title: '创建抓取任务', status: 'pending', summary: '等待' },
      { id: 'scrape', role: 'Scraper', title: '深度全量抓取', status: 'pending', summary: '等待' },
    ])

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

    try {
      // One API = full pipeline. Intermediate steps returned for UI.
      const result = await apiJSON<{
        plan: ChatMessage['plan']
        job_ids: string[]
        message: string
        steps: PipelineStep[]
        model: string
        source: string
      }>('/api/v1/agent/dispatch', {
        method: 'POST',
        body: JSON.stringify({ goal: goal.trim(), ui_lang: 'zh' }),
      })

      setLiveSteps(result.steps || [])
      const n = result.plan?.tasks?.length || 0
      const src =
        result.source === 'ai'
          ? `真实 AI（${result.model}）`
          : `规则引擎（未配置 GRSAI_API_KEY；默认模型 ${result.model}）`

      const assistant: ChatMessage = {
        id: crypto.randomUUID(),
        role: 'assistant',
        text: `已按标准流程跑完规划并创建 ${n} 个深度全量子任务。\n引擎：${src}\n抓取进行中；下方汇总表会随结果更新，点击行可展开背调。`,
        plan: result.plan,
        steps: result.steps,
        jobIds: result.job_ids,
        model: result.model,
        source: result.source,
        createdAt: Date.now(),
      }
      patchActive((s) => ({
        ...s,
        jobIds: result.job_ids || [],
        messages: [...s.messages, assistant],
      }))
    } catch (e) {
      setLiveSteps([])
      patchActive((s) => ({
        ...s,
        messages: [
          ...s.messages,
          {
            id: crypto.randomUUID(),
            role: 'assistant',
            text: '处理失败',
            error: e instanceof Error ? e.message : String(e),
            createdAt: Date.now(),
          },
        ],
      }))
    } finally {
      setBusy(false)
    }
  }

  const empty = !active?.messages.length
  const shuffleSuggestions = () => {
    setSuggestions((prev) => {
      const next = [...prev]
      for (let i = next.length - 1; i > 0; i--) {
        const j = Math.floor(Math.random() * (i + 1))
        ;[next[i], next[j]] = [next[j], next[i]]
      }
      return next
    })
  }

  return (
    <TooltipProvider>
      <div className="flex h-full min-h-0 bg-background">
        {/* Left: 新建任务 + 历史 */}
        <aside
          className={cn(
            'flex h-full shrink-0 flex-col border-r border-sidebar-border bg-sidebar transition-[width]',
            sidebarOpen ? 'w-[260px]' : 'w-0 overflow-hidden border-0',
          )}
        >
          <div className="px-3 pt-4 pb-2">
            <Button className="w-full justify-start gap-2 rounded-xl" onClick={createTask}>
              <MessageSquarePlus className="size-4" />
              新建任务
            </Button>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-4">
            <p className="px-2 py-2 text-xs font-medium text-muted-foreground">历史任务</p>
            <div className="space-y-1">
              {sessions.map((s) => (
                <button
                  key={s.id}
                  type="button"
                  onClick={() => {
                    setActiveId(s.id)
                    setLiveSteps([])
                  }}
                  className={cn(
                    'flex w-full flex-col rounded-xl px-3 py-2.5 text-left transition-colors',
                    s.id === activeId ? 'bg-sidebar-accent text-sidebar-accent-foreground' : 'hover:bg-muted',
                  )}
                >
                  <span className="truncate text-sm font-medium">{s.title}</span>
                  <span className="truncate text-[11px] text-muted-foreground">
                    {new Date(s.updatedAt).toLocaleString()}
                  </span>
                </button>
              ))}
            </div>
          </div>
        </aside>

        <main className="flex min-w-0 flex-1 flex-col">
          {/* Top bar */}
          <header className="flex items-center justify-between px-5 py-3">
            <Button
              size="icon"
              variant="ghost"
              className="rounded-full"
              onClick={() => setSidebarOpen((v) => !v)}
            >
              {sidebarOpen ? <PanelLeftClose className="size-4" /> : <Menu className="size-4" />}
            </Button>
            <div className="flex items-center gap-2">
              <Button size="icon" variant="ghost" className="rounded-full" aria-label="通知">
                <Bell className="size-4" />
              </Button>
              <div className="flex size-9 items-center justify-center rounded-full bg-primary text-primary-foreground">
                <UserRound className="size-4" />
              </div>
            </div>
          </header>

          <Conversation className="min-h-0">
            <ConversationContent className="mx-auto w-full max-w-5xl gap-6 px-4 pb-4 md:px-8">
              {empty ? (
                <>
                  {/* Hero — 1:1 mockup */}
                  <section className="grid items-center gap-6 md:grid-cols-[1.15fr_0.85fr]">
                    <div>
                      <h1 className="text-3xl font-semibold tracking-tight md:text-4xl">
                        你好，我是
                        <span className="text-primary">地图获客助手</span>
                      </h1>
                      <p className="mt-3 max-w-xl text-sm leading-relaxed text-muted-foreground md:text-base">
                        用自然语言描述「在哪里找什么」，系统自动按标准 Agent 流程深度全量找客户。
                        <br />
                        精准线索、结果汇总、出结果后再背调。
                      </p>
                      <div className="mt-5 flex flex-wrap gap-4">
                        {[
                          { icon: '/agent/decor/feat-pin.png', title: '精准定位', desc: '国家/地点/半径全覆盖' },
                          { icon: '/agent/decor/feat-db.png', title: '海量线索', desc: '深度网格不限数量' },
                          { icon: '/agent/decor/feat-ai.png', title: '智能分析', desc: '结果表 + 按需背调' },
                        ].map((f) => (
                          <div key={f.title} className="flex min-w-[140px] items-start gap-2">
                            <img src={f.icon} alt="" className="size-9 rounded-full object-cover" />
                            <div>
                              <div className="text-sm font-semibold">{f.title}</div>
                              <div className="text-xs text-muted-foreground">{f.desc}</div>
                            </div>
                          </div>
                        ))}
                      </div>
                    </div>
                    <div className="relative mx-auto w-full max-w-md">
                      <img
                        src="/agent/decor/hero-map.png"
                        alt="地图获客示意"
                        className="w-full rounded-3xl object-cover shadow-sm"
                      />
                    </div>
                  </section>

                  {/* Quick start */}
                  <section>
                    <h2 className="mb-3 text-base font-semibold">快速开始</h2>
                    <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
                      {QUICK_START.map((card) => (
                        <button
                          key={card.title}
                          type="button"
                          className="group rounded-2xl border border-border bg-card p-4 text-left shadow-sm transition hover:-translate-y-0.5 hover:shadow-md"
                          onClick={() => {
                            if (card.href) window.location.href = card.href
                            else if (card.prompt) runGoal(card.prompt)
                          }}
                        >
                          <img src={card.icon} alt="" className="mb-3 size-10 rounded-xl object-cover" />
                          <div className="text-sm font-semibold">{card.title}</div>
                          <p className="mt-1 text-xs leading-relaxed text-muted-foreground">{card.desc}</p>
                          <ArrowRight className="mt-3 size-4 text-primary opacity-70 transition group-hover:translate-x-0.5" />
                        </button>
                      ))}
                    </div>
                  </section>

                  {/* Suggestions */}
                  <section>
                    <div className="mb-3 flex items-center justify-between">
                      <h2 className="text-base font-semibold">你可以这样问</h2>
                      <button
                        type="button"
                        className="inline-flex items-center gap-1 text-xs text-primary"
                        onClick={shuffleSuggestions}
                      >
                        <RefreshCw className="size-3.5" /> 换一换
                      </button>
                    </div>
                    <Suggestions className="grid w-full grid-cols-1 gap-2 md:grid-cols-2">
                      {suggestions.slice(0, 6).map((s) => (
                        <Suggestion
                          key={s}
                          suggestion={s}
                          onClick={(v) => runGoal(v)}
                          className="h-auto w-full justify-between rounded-xl px-4 py-3 text-left font-normal whitespace-normal"
                        >
                          <span className="pr-2">{s}</span>
                          <ArrowRight className="size-4 shrink-0 text-primary" />
                        </Suggestion>
                      ))}
                    </Suggestions>
                  </section>
                </>
              ) : (
                active?.messages.map((m) => (
                  <Message key={m.id} from={m.role}>
                    <MessageContent>
                      <MessageResponse>{m.text}</MessageResponse>
                      {m.error && <p className="mt-2 text-sm text-destructive">{m.error}</p>}

                      {m.role === 'assistant' && !!m.steps?.length && (
                        <div className="mt-4 space-y-3">
                          <div className="text-xs text-muted-foreground">
                            模型：{m.source === 'ai' ? m.model : `未启用 AI（规则引擎）· 配置模型 ${m.model}`}
                          </div>

                          <Reasoning defaultOpen>
                            <ReasoningTrigger>Agent 中间过程</ReasoningTrigger>
                            <ReasoningContent>
                              {m.steps
                                .map((s) => `### ${s.title}（${s.role}）\n${s.summary}`)
                                .join('\n\n')}
                            </ReasoningContent>
                          </Reasoning>

                          <ChainOfThought defaultOpen>
                            <ChainOfThoughtHeader>标准流程</ChainOfThoughtHeader>
                            <ChainOfThoughtContent>
                              {m.steps.map((s) => (
                                <ChainOfThoughtStep
                                  key={s.id}
                                  label={`${s.title} · ${s.role}`}
                                  description={s.summary}
                                  status={
                                    s.status === 'active'
                                      ? 'active'
                                      : s.status === 'complete'
                                        ? 'complete'
                                        : 'pending'
                                  }
                                />
                              ))}
                            </ChainOfThoughtContent>
                          </ChainOfThought>

                          {!!m.plan?.tasks?.length && (
                            <Task defaultOpen>
                              <TaskTrigger title={`抓取计划 · ${m.plan.tasks.length} 项`} />
                              <TaskContent>
                                {m.plan.tasks.map((t, i) => (
                                  <TaskItem key={`${t.name}-${i}`}>
                                    <div className="flex flex-wrap items-center gap-2">
                                      <span className="font-medium text-foreground">{t.name}</span>
                                      <TaskItemFile>{t.radius_km}km</TaskItemFile>
                                      <TaskItemFile>深度全量</TaskItemFile>
                                    </div>
                                  </TaskItem>
                                ))}
                              </TaskContent>
                            </Task>
                          )}

                          {!!m.jobIds?.length && <ResultsTable jobIds={m.jobIds} />}
                        </div>
                      )}
                    </MessageContent>
                  </Message>
                ))
              )}

              {busy && (
                <Message from="assistant">
                  <MessageContent>
                    <Reasoning isStreaming defaultOpen>
                      <ReasoningTrigger>正在执行标准 Agent 流程…</ReasoningTrigger>
                      <ReasoningContent>
                        {liveSteps.map((s) => `${s.title}: ${s.summary}`).join('\n')}
                      </ReasoningContent>
                    </Reasoning>
                    <ChainOfThought defaultOpen className="mt-3">
                      <ChainOfThoughtHeader>实时步骤</ChainOfThoughtHeader>
                      <ChainOfThoughtContent>
                        {liveSteps.map((s) => (
                          <ChainOfThoughtStep
                            key={s.id}
                            label={s.title}
                            description={s.summary}
                            status={
                              s.status === 'active'
                                ? 'active'
                                : s.status === 'complete'
                                  ? 'complete'
                                  : 'pending'
                            }
                          />
                        ))}
                      </ChainOfThoughtContent>
                    </ChainOfThought>
                  </MessageContent>
                </Message>
              )}
            </ConversationContent>
            <ConversationScrollButton />
          </Conversation>

          {/* Prompt — ai-elements; no fake R1 / web-search */}
          <div className="border-t border-border bg-background/90 px-4 py-3 backdrop-blur md:px-8">
            <div className="mx-auto w-full max-w-3xl">
              <PromptInput
                className="rounded-2xl border border-border bg-card shadow-sm"
                onSubmit={async (msg) => {
                  const text = (msg.text || '').trim()
                  if (text) await runGoal(text)
                }}
              >
                <PromptInputBody>
                  <PromptInputTextarea
                    placeholder="输入你的需求，例如：帮我找雅加达的咖啡馆，尽量找全"
                    className="min-h-[56px]"
                    disabled={busy}
                  />
                </PromptInputBody>
                <PromptInputFooter className="justify-between px-2 pb-2">
                  <div className="text-[11px] text-muted-foreground">
                    {aiMeta
                      ? aiMeta.enabled
                        ? `模型 ${aiMeta.model}`
                        : `AI 未配置 · 规则引擎（${aiMeta.model}）`
                      : '检测模型…'}
                  </div>
                  <PromptInputSubmit disabled={busy} />
                </PromptInputFooter>
              </PromptInput>
              <p className="mt-2 text-center text-[11px] text-muted-foreground">
                内容由 AI / 规则引擎生成，仅供参考
              </p>
            </div>
          </div>
        </main>
      </div>
    </TooltipProvider>
  )
}
