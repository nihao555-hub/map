export type AgentTask = {
  name: string
  country_code: string
  country_name: string
  location: string
  keywords: string[]
  radius_km: number
  enable_intel: boolean
  role: string
}

export type AgentIntent = {
  raw_goal: string
  country_code: string
  country_name: string
  location: string
  keywords: string[]
  radius_km: number
  enable_intel: boolean
  ui_lang: string
  notes?: string
  source: string
}

export type AgentPlan = {
  intent: AgentIntent
  tasks: AgentTask[]
  roles: string[]
}

export type PipelineStep = {
  id: string
  role: string
  title: string
  status: string
  summary: string
  detail?: unknown
}

export type ChatMessage = {
  id: string
  role: 'user' | 'assistant'
  text: string
  plan?: AgentPlan
  steps?: PipelineStep[]
  jobIds?: string[]
  model?: string
  source?: string
  error?: string
  createdAt: number
}

export type AgentSession = {
  id: string
  title: string
  createdAt: number
  updatedAt: number
  messages: ChatMessage[]
  jobIds: string[]
}

const KEY = 'gms_agent_sessions_v2'

export function loadSessions(): AgentSession[] {
  try {
    const raw = localStorage.getItem(KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw) as AgentSession[]
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

export function saveSessions(sessions: AgentSession[]) {
  localStorage.setItem(KEY, JSON.stringify(sessions.slice(0, 40)))
}

export function newSession(): AgentSession {
  const now = Date.now()
  return {
    id: crypto.randomUUID(),
    title: '新任务',
    createdAt: now,
    updatedAt: now,
    messages: [],
    jobIds: [],
  }
}

export function titleFromGoal(goal: string) {
  const t = goal.trim().replace(/\s+/g, ' ')
  return t.length > 28 ? t.slice(0, 28) + '…' : t || '新任务'
}
