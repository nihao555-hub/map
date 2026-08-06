/**
 * Adapted from Vercel AI Elements (task)
 * https://github.com/vercel/ai-elements
 */
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { cn } from '@/lib/utils'
import { ChevronDownIcon, SearchIcon } from 'lucide-react'
import type { ComponentProps } from 'react'

export const TaskItemFile = ({
  children,
  className,
  ...props
}: ComponentProps<'div'>) => (
  <div
    className={cn(
      'inline-flex items-center gap-1 rounded-md border border-[var(--color-border)] bg-[var(--color-secondary)] px-1.5 py-0.5 text-xs',
      className,
    )}
    {...props}
  >
    {children}
  </div>
)

export const TaskItem = ({ children, className, ...props }: ComponentProps<'div'>) => (
  <div className={cn('text-sm text-[var(--color-muted-foreground)]', className)} {...props}>
    {children}
  </div>
)

export const Task = ({
  defaultOpen = true,
  className,
  ...props
}: ComponentProps<typeof Collapsible>) => (
  <Collapsible className={cn(className)} defaultOpen={defaultOpen} {...props} />
)

export const TaskTrigger = ({
  children,
  className,
  title,
  ...props
}: ComponentProps<typeof CollapsibleTrigger> & { title: string }) => (
  <CollapsibleTrigger asChild className={cn('group', className)} {...props}>
    {children ?? (
      <div className="flex w-full cursor-pointer items-center gap-2 text-sm text-[var(--color-muted-foreground)] transition-colors hover:text-[var(--color-foreground)]">
        <SearchIcon className="size-4" />
        <p className="text-sm">{title}</p>
        <ChevronDownIcon className="size-4 transition-transform group-data-[state=open]:rotate-180" />
      </div>
    )}
  </CollapsibleTrigger>
)

export const TaskContent = ({
  children,
  className,
  ...props
}: ComponentProps<typeof CollapsibleContent>) => (
  <CollapsibleContent className={cn('outline-none', className)} {...props}>
    <div className="mt-3 space-y-2 border-l-2 border-[var(--color-border)] pl-4">{children}</div>
  </CollapsibleContent>
)
