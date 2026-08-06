/**
 * Adapted from Vercel AI Elements (message)
 * https://github.com/vercel/ai-elements
 */
import { cn } from '@/lib/utils'
import type { HTMLAttributes } from 'react'

export type MessageProps = HTMLAttributes<HTMLDivElement> & {
  from: 'user' | 'assistant' | 'system'
}

export const Message = ({ className, from, ...props }: MessageProps) => (
  <div
    className={cn(
      'group flex w-full max-w-3xl flex-col gap-2',
      from === 'user' ? 'is-user ml-auto items-end' : 'is-assistant',
      className,
    )}
    {...props}
  />
)

export type MessageContentProps = HTMLAttributes<HTMLDivElement>

export const MessageContent = ({ children, className, ...props }: MessageContentProps) => (
  <div
    className={cn(
      'flex w-fit min-w-0 max-w-full flex-col gap-2 overflow-hidden text-sm leading-relaxed',
      'group-[.is-user]:rounded-2xl group-[.is-user]:bg-[var(--color-secondary)] group-[.is-user]:px-4 group-[.is-user]:py-3',
      'group-[.is-assistant]:w-full group-[.is-assistant]:text-[var(--color-foreground)]',
      className,
    )}
    {...props}
  >
    {children}
  </div>
)

export const MessageResponse = ({
  children,
  className,
  ...props
}: HTMLAttributes<HTMLDivElement>) => (
  <div className={cn('whitespace-pre-wrap break-words', className)} {...props}>
    {children}
  </div>
)
