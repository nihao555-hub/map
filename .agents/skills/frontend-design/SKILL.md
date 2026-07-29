---
name: frontend-design
description: Build accessible, responsive interfaces with explicit design tokens, clear hierarchy, restrained color, and purposeful motion.
---

# Frontend design guidance

Attribution: condensed adaptation of [Google's DESIGN.md format and design-system guidance](https://github.com/google-labs-code/design.md), licensed under the Apache License 2.0. This project-specific adaptation is written in our own words rather than copied verbatim.

## Use a small, explicit system

- Define colors, typography, spacing, radii, shadows, and component states as named tokens before styling components.
- Use a neutral ramp for most surfaces and text, with one accent color reserved for actions, links, focus, and progress.
- Prefer a 4px-based spacing scale such as 4, 8, 12, 16, 24, 32, 48, and 64px. Avoid one-off spacing values.
- Use a type scale with a clear heading/body/label hierarchy. Use `rem` for type and unitless line heights.
- Keep readable lines near 80 characters and keep primary content left aligned unless centering has a clear purpose.

## Component rules

- Components need complete states: default, hover, active, focus-visible, disabled, loading, success, and error where relevant.
- Cards group one task or idea. Use restrained borders and shadows; do not use decoration that competes with the action.
- Buttons and inputs should be comfortably tappable (at least 44px high), with labels and error text that remain understandable without color.
- Tables should preserve the useful columns first, support narrow screens with scrolling, and keep headers visually anchored.
- Progress must communicate both state and measured facts; an indeterminate animation is supplementary, never the only signal.

## Accessibility and motion

- Keep text/background contrast at WCAG AA levels and do not communicate status with color alone.
- Provide visible `:focus-visible` rings and preserve keyboard navigation.
- Respect `prefers-reduced-motion: reduce`; disable non-essential transitions and animations.
- Provide dark-mode tokens through `prefers-color-scheme: dark` without changing semantic meaning.
- Prefer semantic headings, labels, lists, fieldsets, and live regions over visual-only markup.

## Review checklist

1. Can a first-time user find the primary action immediately?
2. Are spacing, colors, and typography tokenized rather than scattered literals?
3. Do all interactive controls expose hover, focus, active, disabled, and loading states?
4. Does the interface remain usable at mobile widths, high zoom, keyboard-only input, dark mode, and reduced motion?
