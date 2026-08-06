/**
 * Simplified prompt composer inspired by Vercel AI Elements (prompt-input)
 * https://github.com/vercel/ai-elements
 */
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { ArrowUpIcon, SquareIcon } from 'lucide-react'
import type { FormEvent, KeyboardEvent } from 'react'
import { useState } from 'react'

export type PromptInputProps = {
  className?: string
  placeholder?: string
  disabled?: boolean
  busy?: boolean
  value?: string
  onValueChange?: (v: string) => void
  onSubmit?: (text: string) => void
  onStop?: () => void
}

export function PromptInput({
  className,
  placeholder = '描述你的获客目标…',
  disabled,
  busy,
  value: controlled,
  onValueChange,
  onSubmit,
  onStop,
}: PromptInputProps) {
  const [inner, setInner] = useState('')
  const value = controlled ?? inner
  const setValue = (v: string) => {
    if (controlled === undefined) setInner(v)
    onValueChange?.(v)
  }

  const submit = (e?: FormEvent) => {
    e?.preventDefault()
    const text = value.trim()
    if (!text || disabled || busy) return
    onSubmit?.(text)
    if (controlled === undefined) setInner('')
  }

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      submit()
    }
  }

  return (
    <form
      onSubmit={submit}
      className={cn(
        'rounded-2xl border border-[var(--color-border)] bg-[var(--color-card)] p-3 shadow-sm',
        className,
      )}
    >
      <textarea
        rows={2}
        value={value}
        disabled={disabled}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={onKeyDown}
        placeholder={placeholder}
        className="max-h-40 min-h-[56px] w-full resize-none bg-transparent px-1 py-1 text-sm outline-none placeholder:text-[var(--color-muted-foreground)]"
      />
      <div className="mt-2 flex items-center justify-between gap-2">
        <p className="text-xs text-[var(--color-muted-foreground)]">Enter 发送 · Shift+Enter 换行</p>
        {busy ? (
          <Button type="button" size="icon" variant="secondary" onClick={onStop} aria-label="停止">
            <SquareIcon className="size-4" />
          </Button>
        ) : (
          <Button
            type="submit"
            size="icon"
            disabled={!value.trim() || disabled}
            aria-label="发送"
          >
            <ArrowUpIcon className="size-4" />
          </Button>
        )}
      </div>
    </form>
  )
}
