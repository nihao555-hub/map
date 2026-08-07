import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from 'react'
import {
  ArrowRight,
  Clock3,
  FileText,
  Plus,
  RefreshCw,
  Trash2,
} from 'lucide-react'
import {
  Conversation,
  ConversationContent,
  ConversationScrollButton,
} from '@/components/ai-elements/conversation'
import { Message, MessageContent, MessageResponse } from '@/components/ai-elements/message'
import {
  Reasoning,
  ReasoningContent,
  ReasoningTrigger,
} from '@/components/ai-elements/reasoning'
import { Task, TaskContent, TaskItem, TaskItemFile, TaskTrigger } from '@/components/ai-elements/task'
import { Suggestion } from '@/components/ai-elements/suggestion'
import {
  PromptInput,
  PromptInputBody,
  PromptInputFooter,
  PromptInputSubmit,
  PromptInputTextarea,
} from '@/components/ai-elements/prompt-input'
import { TooltipProvider } from '@/components/ui/tooltip'
import {
  loadSessions,
  newSession,
  saveSessions,
  titleFromGoal,
  type AgentSession,
  type ChatMessage,
  type JobMeta,
} from '@/lib/sessions'
import { cn, createId } from '@/lib/utils'

const ResultsTable = lazy(() =>
  import('@/components/ResultsTable').then((module) => ({ default: module.ResultsTable })),
)

/** Only real capabilities — no mock persona/marketing cards. */
const SUGGESTIONS = [
  '覆盖整个印尼，找全所有配电柜、配电设备、电气分销商和开关柜供应商',
  '帮我找雅加达的咖啡馆，尽量找全',
  '覆盖整个雅加达找进口商和咖啡馆',
  '在曼谷找美容店，半径15公里',
  'Find importers in Surabaya within 20km',
  '在吉隆坡找咖啡馆，尽量找全',
]

const QUICK_START = [
  {
    title: '智能搜索线索',
    desc: '一句话描述地点与品类，自动深度全量抓取',
    icon: '/agent/decor/qs-search.png',
    color: '#2F6BFF',
    prompt: '帮我找雅加达的咖啡馆，尽量找全',
  },
  {
    title: '地图区域获客',
    desc: '打开地图标准界面，圈选区域后继续搜索',
    icon: '/agent/decor/qs-map.png',
    color: '#22C55E',
    href: '/',
  },
]

const FEATURES = [
  { title: '精准定位', desc: '多维度筛选目标客户', icon: '/agent/decor/feat-pin.png' },
  { title: '海量线索', desc: '覆盖目标区域商户数据', icon: '/agent/decor/feat-db.png' },
  { title: '智能分析', desc: '结果汇总与自动背调', icon: '/agent/decor/feat-ai.png' },
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
  const [busy, setBusy] = useState(false)
  const [streamThinking, setStreamThinking] = useState('')
  const [aiMeta, setAiMeta] = useState<{ enabled: boolean; model: string } | null>(null)
  const [suggestions, setSuggestions] = useState(SUGGESTIONS)
  const [showHistory, setShowHistory] = useState(true)

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
    setStreamThinking('')
    setShowHistory(true)
  }

  const clearHistory = () => {
    if (!confirm('确定清空历史任务？')) return
    const s = newSession()
    setSessions([s])
    setActiveId(s.id)
    setStreamThinking('')
  }

  /**
   * Complete agent flow with SSE streaming thinking (real LLM).
   */
  const runGoal = async (goal: string) => {
    if (!goal.trim() || busy) return
    if (aiMeta && !aiMeta.enabled) {
      alert('未配置真实 AI（GRSAI_API_KEY）。请先在环境中配置后再试。')
      return
    }
    setBusy(true)
    setStreamThinking('')

    const userMsg: ChatMessage = {
      id: createId(),
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
      const res = await fetch('/api/v1/agent/dispatch/stream', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ goal: goal.trim(), ui_lang: 'zh' }),
      })
      if (!res.ok) {
        const err = await res.json().catch(() => ({}))
        throw new Error((err as { message?: string }).message || res.statusText)
      }
      if (!res.body) throw new Error('浏览器不支持流式响应')

      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      let thinkingAcc = ''
      let finalized = false

      const handleEvent = (raw: string) => {
        let ev: {
          type: string
          text?: string
          message?: string
          thinking?: string
          plan?: ChatMessage['plan']
          job_ids?: string[]
          model?: string
          source?: string
        }
        try {
          ev = JSON.parse(raw)
        } catch {
          return
        }
        if (ev.type === 'thinking_delta' && ev.text) {
          thinkingAcc += ev.text
          setStreamThinking(thinkingAcc)
          return
        }
        if (ev.type === 'error') {
          throw new Error(ev.text || '流式任务失败')
        }
        if (ev.type === 'result') {
          if (ev.source && ev.source !== 'ai') {
            throw new Error('未走真实 AI，请检查 GRSAI_API_KEY')
          }
          const jobs: JobMeta[] = (ev.job_ids || []).map((id, i) => ({
            id,
            name: ev.plan?.tasks?.[i]?.name || `任务 ${i + 1}`,
          }))
          const assistant: ChatMessage = {
            id: createId(),
            role: 'assistant',
            text:
              ev.message ||
              `已启动 ${jobs.length} 个子任务，结果会汇总到下方表格。`,
            thinking: ev.thinking || thinkingAcc,
            plan: ev.plan,
            jobs,
            jobIds: ev.job_ids,
            model: ev.model,
            source: ev.source,
            createdAt: Date.now(),
          }
          patchActive((s) => ({
            ...s,
            jobs,
            messages: [...s.messages, assistant],
          }))
          finalized = true
          setStreamThinking('')
        }
      }

      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const parts = buffer.split('\n\n')
        buffer = parts.pop() || ''
        for (const part of parts) {
          const line = part
            .split('\n')
            .map((l) => l.trim())
            .find((l) => l.startsWith('data:'))
          if (!line) continue
          handleEvent(line.slice(5).trim())
        }
      }
      if (buffer.trim()) {
        const line = buffer
          .split('\n')
          .map((l) => l.trim())
          .find((l) => l.startsWith('data:'))
        if (line) handleEvent(line.slice(5).trim())
      }
      if (!finalized) throw new Error('流式响应未完成')
    } catch (e) {
      setStreamThinking('')
      patchActive((s) => ({
        ...s,
        messages: [
          ...s.messages,
          {
            id: createId(),
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

  return (
    <TooltipProvider>
      <div className="flex h-full min-h-0 bg-[#EEF2F8]">
        {/* ===== Sidebar — 1:1 mockup ===== */}
        <aside className="flex w-[248px] shrink-0 flex-col bg-white shadow-[1px_0_0_#E8EEF6]">
          <div className="flex items-center gap-3 px-5 pt-6 pb-5">
            <img
              src="/agent/decor/logo-m.webp"
              alt=""
              width={40}
              height={40}
              className="size-10 shrink-0 rounded-full object-cover shadow-[0_2px_8px_rgba(47,107,255,0.25)]"
            />
            <div className="text-[16px] font-bold tracking-tight text-[#111827]">地图获客助手</div>
          </div>

          <div className="space-y-2.5 px-4">
            <button
              type="button"
              onClick={createTask}
              className="flex h-11 w-full items-center justify-center gap-2 rounded-full bg-[#2F6BFF] text-[14px] font-medium text-white shadow-[0_4px_14px_rgba(47,107,255,0.35)] transition hover:bg-[#2563EB] active:scale-[0.99]"
            >
              <Plus className="size-4 stroke-[2.5]" />
              新建任务
            </button>
            <button
              type="button"
              onClick={() => setShowHistory((v) => !v)}
              className={cn(
                'flex h-11 w-full items-center justify-center gap-2 rounded-full text-[14px] font-medium transition',
                showHistory
                  ? 'bg-[#EAF1FF] text-[#2563EB]'
                  : 'bg-[#F3F5F9] text-[#4B5563] hover:bg-[#E8ECF3]',
              )}
            >
              <Clock3 className="size-4" />
              历史任务
            </button>
          </div>

          <div className="mt-5 min-h-0 flex-1 overflow-y-auto px-3">
            {showHistory && (
              <>
                <p className="px-2 pb-2 text-[12px] font-medium text-[#9CA3AF]">历史任务</p>
                <div className="space-y-0.5">
                  {sessions.map((s) => (
                    <button
                      key={s.id}
                      type="button"
                      onClick={() => {
                        setActiveId(s.id)
                        setStreamThinking('')
                      }}
                      className={cn(
                        'flex w-full items-start gap-2.5 rounded-[12px] px-2.5 py-2.5 text-left transition',
                        s.id === activeId ? 'bg-[#F0F5FF]' : 'hover:bg-[#F7F9FC]',
                      )}
                    >
                      <FileText className="mt-0.5 size-[18px] shrink-0 text-[#2F6BFF]" strokeWidth={1.75} />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-[13px] font-medium text-[#1F2937]">
                          {s.title}
                        </span>
                        <span className="mt-0.5 block text-[11px] leading-none text-[#9CA3AF]">
                          {new Date(s.updatedAt).toLocaleString('zh-CN', {
                            year: 'numeric',
                            month: '2-digit',
                            day: '2-digit',
                            hour: '2-digit',
                            minute: '2-digit',
                            hour12: false,
                          })}
                        </span>
                      </span>
                    </button>
                  ))}
                </div>
              </>
            )}
          </div>

          <div className="p-4">
            <button
              type="button"
              onClick={clearHistory}
              className="flex h-10 w-full items-center justify-center gap-2 rounded-full text-[13px] text-[#6B7280] transition hover:bg-[#F3F4F6] hover:text-[#EF4444]"
            >
              <Trash2 className="size-3.5" />
              清空历史记录
            </button>
          </div>
        </aside>

        {/* ===== Main ===== */}
        <main className="flex min-w-0 flex-1 flex-col">
          <Conversation className="min-h-0">
            <ConversationContent className="mx-auto w-full max-w-[1280px] gap-8 px-4 py-7 md:px-8">
              {empty ? (
                <>
                  {/* Hero — brand + copy left, decor right */}
                  <section className="grid items-center gap-4 md:grid-cols-[minmax(0,1.05fr)_minmax(0,0.95fr)] md:gap-2">
                    <div className="animate-in fade-in slide-in-from-left-2 duration-500">
                      <h1 className="text-[30px] leading-[1.25] font-bold tracking-tight text-[#1E3A8A] md:text-[34px]">
                        你好，我是地图获客助手
                      </h1>
                      <p className="mt-3 max-w-[420px] text-[14px] leading-relaxed text-[#6B7280] md:text-[15px]">
                        我可以帮你在地图上发现潜在客户，获取精准线索，让获客更简单高效！
                      </p>
                      <div className="mt-7 flex flex-wrap gap-x-6 gap-y-4">
                        {FEATURES.map((f) => (
                          <div key={f.title} className="flex min-w-[150px] items-center gap-2.5">
                            <img
                              src={f.icon}
                              alt=""
                              width={40}
                              height={40}
                              loading="lazy"
                              className="size-10 object-contain"
                            />
                            <div>
                              <div className="text-[13px] font-semibold text-[#1F2937]">{f.title}</div>
                              <div className="text-[11px] text-[#9CA3AF]">{f.desc}</div>
                            </div>
                          </div>
                        ))}
                      </div>
                    </div>
                    <div className="animate-in fade-in slide-in-from-right-3 duration-700 mx-auto w-full max-w-[460px]">
                      <img
                        src="/agent/decor/hero-map.webp"
                        alt=""
                        width={900}
                        height={600}
                        fetchPriority="high"
                        className="w-full object-contain drop-shadow-[0_12px_40px_rgba(47,107,255,0.12)]"
                      />
                    </div>
                  </section>

                  {/* Quick start — only shipped capabilities */}
                  <section className="animate-in fade-in slide-in-from-bottom-2 duration-500 delay-100">
                    <h2 className="mb-3.5 text-[16px] font-semibold text-[#1F2937]">快速开始</h2>
                    <div className="grid gap-3.5 sm:grid-cols-2">
                      {QUICK_START.map((card) => (
                        <button
                          key={card.title}
                          type="button"
                          className="group relative rounded-[18px] border border-[#E8EEF6] bg-white p-5 text-left shadow-[0_1px_3px_rgba(15,23,42,0.04)] transition hover:-translate-y-0.5 hover:shadow-[0_8px_24px_rgba(47,107,255,0.1)]"
                          onClick={() => {
                            if (card.href) window.location.href = card.href
                            else if (card.prompt) runGoal(card.prompt)
                          }}
                        >
                          <img
                            src={card.icon}
                            alt=""
                            width={48}
                            height={48}
                            loading="lazy"
                            className="mb-3.5 size-12 object-contain"
                          />
                          <div className="text-[14px] font-semibold text-[#111827]">{card.title}</div>
                          <p className="mt-1.5 max-w-[85%] text-[12px] leading-relaxed text-[#6B7280]">
                            {card.desc}
                          </p>
                          <span
                            className="absolute right-4 bottom-4 flex size-7 items-center justify-center rounded-full text-white shadow-sm transition group-hover:scale-105"
                            style={{ background: card.color }}
                          >
                            <ArrowRight className="size-3.5" />
                          </span>
                        </button>
                      ))}
                    </div>
                  </section>

                  {/* Suggestions — 2-col grid like mockup */}
                  <section className="animate-in fade-in slide-in-from-bottom-2 duration-500 delay-150">
                    <div className="mb-3.5 flex items-center justify-between">
                      <h2 className="text-[16px] font-semibold text-[#1F2937]">你可以这样问</h2>
                      <button
                        type="button"
                        className="inline-flex items-center gap-1 text-[12px] font-medium text-[#2F6BFF] hover:text-[#2563EB]"
                        onClick={() =>
                          setSuggestions((prev) => {
                            const next = [...prev]
                            for (let i = next.length - 1; i > 0; i--) {
                              const j = Math.floor(Math.random() * (i + 1))
                              ;[next[i], next[j]] = [next[j], next[i]]
                            }
                            return next
                          })
                        }
                      >
                        <RefreshCw className="size-3.5" /> 换一换
                      </button>
                    </div>
                    <div className="grid grid-cols-1 gap-2.5 md:grid-cols-2">
                      {suggestions.slice(0, 6).map((s) => (
                        <Suggestion
                          key={s}
                          suggestion={s}
                          onClick={(v) => runGoal(v)}
                          className="h-auto w-full justify-between rounded-[14px] border-0 bg-[#F1F3F7] px-4 py-3.5 text-left text-[13px] font-normal whitespace-normal text-[#374151] shadow-none hover:bg-[#E8F0FF]"
                        >
                          <span className="pr-2 leading-snug">{s}</span>
                          <ArrowRight className="size-4 shrink-0 text-[#2F6BFF]" />
                        </Suggestion>
                      ))}
                    </div>
                  </section>
                </>
              ) : (
                active?.messages.map((m) => (
                  <Message key={m.id} from={m.role}>
                    <MessageContent
                      className={
                        m.role === 'assistant' && m.jobs?.length
                          ? 'w-full max-w-full min-w-0 overflow-visible'
                          : 'w-full max-w-full'
                      }
                    >
                      {m.role === 'user' ? (
                        <MessageResponse>{m.text}</MessageResponse>
                      ) : (
                        <>
                          {!!m.thinking && (
                            <Reasoning defaultOpen className="mb-3">
                              <ReasoningTrigger>思考过程</ReasoningTrigger>
                              <ReasoningContent>{m.thinking}</ReasoningContent>
                            </Reasoning>
                          )}

                          <MessageResponse>{m.text}</MessageResponse>
                          {m.error && <p className="mt-2 text-sm text-red-600">{m.error}</p>}

                          {!!m.plan?.tasks?.length && (
                            <Task defaultOpen className="mt-4">
                              <TaskTrigger title={`抓取计划 · ${m.plan.tasks.length} 项`} />
                              <TaskContent>
                                {m.plan.tasks.map((t, i) => (
                                  <TaskItem key={`${t.name}-${i}`}>
                                    <div className="flex flex-col gap-1">
                                      <span className="font-medium text-foreground">{t.name}</span>
                                      <div className="flex flex-wrap items-center gap-2">
                                        <TaskItemFile>{t.location || '—'}</TaskItemFile>
                                        <TaskItemFile>
                                          地图搜：{(t.keywords || []).join(' / ') || '—'}
                                        </TaskItemFile>
                                        <TaskItemFile>约 {t.radius_km} 公里</TaskItemFile>
                                      </div>
                                    </div>
                                  </TaskItem>
                                ))}
                              </TaskContent>
                            </Task>
                          )}

                          {!!m.jobs?.length && (
                            <Suspense
                              fallback={
                                <div className="mt-4 rounded-2xl border border-[#E5E7EB] bg-white px-4 py-8 text-center text-sm text-[#9CA3AF]">
                                  正在加载结果表…
                                </div>
                              }
                            >
                              <ResultsTable jobs={m.jobs} />
                            </Suspense>
                          )}
                        </>
                      )}
                    </MessageContent>
                  </Message>
                ))
              )}

              {busy && (
                <Message from="assistant">
                  <MessageContent className="w-full max-w-full">
                    <Reasoning isStreaming defaultOpen>
                      <ReasoningTrigger>思考中</ReasoningTrigger>
                      <ReasoningContent>
                        {streamThinking || '模型正在思考…'}
                      </ReasoningContent>
                    </Reasoning>
                  </MessageContent>
                </Message>
              )}
            </ConversationContent>
            <ConversationScrollButton />
          </Conversation>

          {/* Input — no mock R1 / web-search / attachment */}
          <div className="bg-transparent px-6 pt-1 pb-4 md:px-12">
            <div className="mx-auto w-full max-w-[760px]">
              <PromptInput
                className="rounded-[22px] border-[1.5px] border-[#93C5FD] bg-white shadow-[0_8px_28px_rgba(47,107,255,0.08)]"
                onSubmit={async (msg) => {
                  const text = (msg.text || '').trim()
                  if (text) await runGoal(text)
                }}
              >
                <PromptInputBody>
                  <PromptInputTextarea
                    placeholder='输入你的需求，例如："帮我找北京市朝阳区的火锅店"'
                    className="min-h-[64px] text-[14px]"
                    disabled={busy}
                  />
                </PromptInputBody>
                <PromptInputFooter className="justify-between px-3 pb-3">
                  <div className="text-[11px] text-[#9CA3AF]">
                    {aiMeta
                      ? aiMeta.enabled
                        ? '真实 AI 流式输出已启用'
                        : 'AI 未配置 · 无法启动真实模型'
                      : '检测模型…'}
                  </div>
                  <PromptInputSubmit
                    disabled={busy}
                    className="size-9 rounded-full bg-[#2F6BFF] hover:bg-[#2563EB]"
                  />
                </PromptInputFooter>
              </PromptInput>
              <p className="mt-2.5 text-center text-[11px] text-[#9CA3AF]">
                内容由 AI 生成，仅供参考
              </p>
            </div>
          </div>
        </main>
      </div>
    </TooltipProvider>
  )
}
