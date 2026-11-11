"use client";

import React, { useMemo } from "react";
import { marked } from "marked";
import DOMPurify from "dompurify";

interface MarkdownViewerProps {
  content: string;
  className?: string;
  maxHeight?: string;
}

export function MarkdownViewer({
  content,
  className = "",
  maxHeight = "600px",
}: MarkdownViewerProps) {
  const htmlContent = useMemo(() => {
    if (!content) return "";
    try {
      const rawHtml = marked.parse(content, {
        gfm: true,
        breaks: true,
      }) as string;

      // Sanitize against XSS attacks while keeping formatting tags
      if (typeof window !== "undefined") {
        return DOMPurify.sanitize(rawHtml, {
          ADD_ATTR: ["target"],
        });
      }
      return rawHtml;
    } catch (err) {
      console.error("Markdown parse error:", err);
      return `<pre>${content}</pre>`;
    }
  }, [content]);

  return (
    <div
      style={{ maxHeight }}
      className={`overflow-y-auto p-5 rounded-xl bg-slate-950 border border-slate-800 text-slate-200 select-text leading-relaxed ${className}`}
    >
      <div
        className="prose prose-invert max-w-none text-xs sm:text-sm space-y-3.5
          [&>h1]:text-lg [&>h1]:font-bold [&>h1]:text-white [&>h1]:pb-2 [&>h1]:border-b [&>h1]:border-slate-800 [&>h1]:mt-6 [&>h1]:mb-3
          [&>h2]:text-base [&>h2]:font-bold [&>h2]:text-sky-300 [&>h2]:mt-5 [&>h2]:mb-2 [&>h2]:border-b [&>h2]:border-slate-800/60 [&>h2]:pb-1.5
          [&>h3]:text-sm [&>h3]:font-semibold [&>h3]:text-slate-200 [&>h3]:mt-4 [&>h3]:mb-1.5
          [&>h4]:text-xs [&>h4]:font-semibold [&>h4]:text-slate-300 [&>h4]:mt-3
          [&>p]:text-slate-300 [&>p]:leading-relaxed [&>p]:mb-2
          [&>ul]:list-disc [&>ul]:list-inside [&>ul]:space-y-1.5 [&>ul]:text-slate-300 [&>ul]:my-2
          [&>ol]:list-decimal [&>ol]:list-inside [&>ol]:space-y-1.5 [&>ol]:text-slate-300 [&>ol]:my-2
          [&>li]:text-slate-300
          [&_blockquote]:border-l-4 [&_blockquote]:border-sky-500/60 [&_blockquote]:bg-sky-500/10 [&_blockquote]:px-3.5 [&_blockquote]:py-2 [&_blockquote]:rounded-r-lg [&_blockquote]:text-slate-300 [&_blockquote]:italic [&_blockquote]:my-3
          [&_code]:font-mono [&_code]:text-[11px] [&_code]:bg-slate-900 [&_code]:border [&_code]:border-slate-800 [&_code]:text-sky-300 [&_code]:px-1.5 [&_code]:py-0.5 [&_code]:rounded
          [&_pre]:bg-slate-900/90 [&_pre]:border [&_pre]:border-slate-800/80 [&_pre]:p-3.5 [&_pre]:rounded-xl [&_pre]:overflow-x-auto [&_pre]:my-3
          [&_pre_code]:bg-transparent [&_pre_code]:border-0 [&_pre_code]:p-0 [&_pre_code]:text-slate-200 [&_pre_code]:text-xs
          [&_table]:w-full [&_table]:border-collapse [&_table]:text-xs [&_table]:my-3
          [&_th]:border [&_th]:border-slate-800 [&_th]:bg-slate-900 [&_th]:p-2.5 [&_th]:text-left [&_th]:font-semibold [&_th]:text-white
          [&_td]:border [&_td]:border-slate-800 [&_td]:p-2.5 [&_td]:text-slate-300
          [&_hr]:border-slate-800 [&_hr]:my-5
          [&_a]:text-sky-400 [&_a]:underline [&_a]:hover:text-sky-300"
        dangerouslySetInnerHTML={{ __html: htmlContent }}
      />
    </div>
  );
}
