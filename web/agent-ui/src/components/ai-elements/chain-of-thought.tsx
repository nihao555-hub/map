/**
 * Adapted from Vercel AI Elements (chain-of-thought)
 * https://github.com/vercel/ai-elements
 */
import { useControllableState } from '@radix-ui/react-use-controllable-state'
import { Badge } from '@/components/ui/badge'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { cn } from '@/lib/utils'
import type { LucideIcon } from 'lucide-react'
import { BrainIcon, ChevronDownIcon, DotIcon } from 'lucide-react'
import type { ComponentProps, ReactNode } from 'react'
import { createContext, memo, useContext, useMemo } from 'react'

interface ChainOfThoughtContextValue {
  isOpen: boolean
  setIsOpen: (open: boolean) => void
}

const ChainOfThoughtContext = createContext<ChainOfThoughtContextValue | null>(null)

const useChainOfThought = () => {
  const context = useContext(ChainOfThoughtContext)
  if (!context) {
    throw new Error('ChainOfThought components must be used within ChainOfThought')
  }
  return context
}

export type ChainOfThoughtProps = ComponentProps<'div'> & {
  open?: boolean
  defaultOpen?: boolean
  onOpenChange?: (open: boolean) => void
}

export const ChainOfThought = memo(
  ({
    className,
    open,
    defaultOpen = false,
    onOpenChange,
    children,
    ...props
  }: ChainOfThoughtProps) => {
    const [isOpen, setIsOpen] = useControllableState({
      defaultProp: defaultOpen,
      onChange: onOpenChange,
      prop: open,
    })

    const value = useMemo(() => ({ isOpen: !!isOpen, setIsOpen }), [isOpen, setIsOpen])

    return (
      <ChainOfThoughtContext.Provider value={value}>
        <div className={cn('not-prose w-full space-y-3', className)} {...props}>
          {children}
        </div>
      </ChainOfThoughtContext.Provider>
    )
  },
)

export const ChainOfThoughtHeader = memo(
  ({ className, children, ...props }: ComponentProps<typeof CollapsibleTrigger>) => {
    const { isOpen, setIsOpen } = useChainOfThought()
    return (
      <Collapsible onOpenChange={setIsOpen} open={isOpen}>
        <CollapsibleTrigger
          className={cn(
            'flex w-full items-center gap-2 text-sm text-[var(--color-muted-foreground)] transition-colors hover:text-[var(--color-foreground)]',
            className,
          )}
          {...props}
        >
          <BrainIcon className="size-4" />
          <span className="flex-1 text-left">{children ?? 'Chain of Thought'}</span>
          <ChevronDownIcon
            className={cn('size-4 transition-transform', isOpen ? 'rotate-180' : 'rotate-0')}
          />
        </CollapsibleTrigger>
      </Collapsible>
    )
  },
)

const stepStatusStyles = {
  active: 'text-[var(--color-foreground)]',
  complete: 'text-[var(--color-muted-foreground)]',
  pending: 'text-[var(--color-muted-foreground)]/50',
}

export const ChainOfThoughtStep = memo(
  ({
    className,
    icon: Icon = DotIcon,
    label,
    description,
    status = 'complete',
    children,
    ...props
  }: ComponentProps<'div'> & {
    icon?: LucideIcon
    label: ReactNode
    description?: ReactNode
    status?: 'complete' | 'active' | 'pending'
  }) => (
    <div
      className={cn('flex gap-2 text-sm', stepStatusStyles[status], className)}
      {...props}
    >
      <div className="relative mt-0.5">
        <Icon
          className={cn(
            'size-4',
            status === 'active' && 'text-[var(--color-primary)]',
          )}
        />
        <div className="absolute top-7 bottom-0 left-1/2 -mx-px w-px bg-[var(--color-border)]" />
      </div>
      <div className="min-w-0 flex-1 space-y-1 overflow-hidden pb-3">
        <div className="font-medium">{label}</div>
        {description && (
          <div className="text-xs text-[var(--color-muted-foreground)]">{description}</div>
        )}
        {children}
      </div>
    </div>
  ),
)

export const ChainOfThoughtSearchResults = memo(
  ({ className, ...props }: ComponentProps<'div'>) => (
    <div className={cn('flex flex-wrap items-center gap-2', className)} {...props} />
  ),
)

export const ChainOfThoughtSearchResult = memo(
  ({ className, children, ...props }: ComponentProps<typeof Badge>) => (
    <Badge
      className={cn('gap-1 px-2 py-0.5 text-xs font-normal', className)}
      variant="secondary"
      {...props}
    >
      {children}
    </Badge>
  ),
)

export const ChainOfThoughtContent = memo(
  ({ className, children, ...props }: ComponentProps<typeof CollapsibleContent>) => {
    const { isOpen } = useChainOfThought()
    return (
      <Collapsible open={isOpen}>
        <CollapsibleContent className={cn('mt-2 space-y-1 outline-none', className)} {...props}>
          {children}
        </CollapsibleContent>
      </Collapsible>
    )
  },
)

ChainOfThought.displayName = 'ChainOfThought'
ChainOfThoughtHeader.displayName = 'ChainOfThoughtHeader'
ChainOfThoughtStep.displayName = 'ChainOfThoughtStep'
ChainOfThoughtSearchResults.displayName = 'ChainOfThoughtSearchResults'
ChainOfThoughtSearchResult.displayName = 'ChainOfThoughtSearchResult'
ChainOfThoughtContent.displayName = 'ChainOfThoughtContent'
