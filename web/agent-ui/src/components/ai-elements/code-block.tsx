"use client";

import { cn } from "@/lib/utils";
import type { HTMLAttributes } from "react";

export type CodeBlockProps = HTMLAttributes<HTMLPreElement> & {
  code: string;
  language?: string;
};

/** Lightweight code block (no Shiki) to keep agent UI bundle small. */
export const CodeBlock = ({ code, className, language, ...props }: CodeBlockProps) => (
  <pre
    className={cn(
      "overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs leading-relaxed",
      className
    )}
    data-language={language}
    {...props}
  >
    <code>{code}</code>
  </pre>
);
