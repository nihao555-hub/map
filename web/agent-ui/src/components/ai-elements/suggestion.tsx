/**
 * Adapted from Vercel AI Elements (suggestion)
 * https://github.com/vercel/ai-elements
 */
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import type { ComponentProps } from 'react'
import { useCallback } from 'react'

export const Suggestions = ({ className, children, ...props }: ComponentProps<'div'>) => (
  <div
    className={cn('flex w-full flex-wrap items-center justify-center gap-2', className)}
    {...props}
  >
    {children}
  </div>
)

export type SuggestionProps = Omit<ComponentProps<typeof Button>, 'onClick'> & {
  suggestion: string
  onClick?: (suggestion: string) => void
}

export const Suggestion = ({
  suggestion,
  onClick,
  className,
  variant = 'outline',
  size = 'sm',
  children,
  ...props
}: SuggestionProps) => {
  const handleClick = useCallback(() => {
    onClick?.(suggestion)
  }, [onClick, suggestion])

  return (
    <Button
      className={cn('cursor-pointer rounded-full px-4', className)}
      onClick={handleClick}
      size={size}
      type="button"
      variant={variant}
      {...props}
    >
      {children || suggestion}
    </Button>
  )
}
