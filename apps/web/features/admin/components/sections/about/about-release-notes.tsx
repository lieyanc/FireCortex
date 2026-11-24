"use client";

import { Streamdown, type Components } from "streamdown";

function safeReleaseNotesURL(rawURL: string): string {
  const trimmed = rawURL.trim();
  if (!trimmed) return "";
  try {
    const parsed = new URL(trimmed);
    if (parsed.protocol === "https:" || parsed.protocol === "http:") {
      return parsed.toString();
    }
  } catch {
    return "";
  }
  return "";
}

const RELEASE_NOTES_COMPONENTS: Components = {
  h1: ({ children }) => <h1 className="text-sm font-semibold text-foreground">{children}</h1>,
  h2: ({ children }) => <h2 className="text-sm font-semibold text-foreground">{children}</h2>,
  h3: ({ children }) => <h3 className="pt-1.5 text-xs font-semibold text-foreground">{children}</h3>,
  h4: ({ children }) => <h4 className="pt-1 text-xs font-medium text-foreground">{children}</h4>,
  p: ({ children }) => <p className="text-xs leading-5 text-muted-foreground">{children}</p>,
  ul: ({ children }) => <ul className="flex list-disc flex-col gap-1.5 pl-4 text-xs leading-5 text-foreground">{children}</ul>,
  ol: ({ children }) => <ol className="flex list-decimal flex-col gap-1.5 pl-4 text-xs leading-5 text-foreground">{children}</ol>,
  li: ({ children }) => <li className="pl-0.5 marker:text-muted-foreground">{children}</li>,
  strong: ({ children }) => <strong className="font-semibold text-foreground">{children}</strong>,
  blockquote: ({ children }) => (
    <blockquote className="border-l-2 border-border pl-3 text-xs text-muted-foreground">{children}</blockquote>
  ),
  hr: () => <hr className="border-border/60" />,
  pre: ({ children }) => (
    <pre className="overflow-x-auto rounded-md bg-muted px-2.5 py-2 font-mono text-[11px] leading-5 text-foreground [&_code]:bg-transparent [&_code]:p-0">
      {children}
    </pre>
  ),
  code: ({ children }) => (
    <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] text-foreground">{children}</code>
  ),
  a: ({ href, children }) => {
    const safeHref = typeof href === "string" ? safeReleaseNotesURL(href) : "";
    if (!safeHref) {
      return <span>{children}</span>;
    }
    return (
      <a
        href={safeHref}
        target="_blank"
        rel="noopener noreferrer"
        className="rounded-sm text-primary underline decoration-primary/30 underline-offset-4 hover:decoration-primary focus-visible:outline-2 focus-visible:outline-ring"
      >
        {children}
      </a>
    );
  },
};

export function AboutReleaseNotes({ notes, label }: { notes: string; label: string }) {
  return (
    <section
      aria-label={label}
      // biome-ignore lint/a11y/noNoninteractiveTabindex: scrollable region needs keyboard focus for arrow/page scrolling
      tabIndex={0}
      className="max-h-64 overflow-y-auto overscroll-contain rounded-md border border-border/60 bg-background/60 px-3 py-2.5 outline-none focus-visible:ring-2 focus-visible:ring-ring"
    >
      <Streamdown
        mode="static"
        parseIncompleteMarkdown={false}
        skipHtml
        rehypePlugins={[]}
        disallowedElements={["img"]}
        urlTransform={safeReleaseNotesURL}
        controls={false}
        linkSafety={{ enabled: false }}
        animated={false}
        isAnimating={false}
        components={RELEASE_NOTES_COMPONENTS}
        className="flex flex-col gap-2 break-words [overflow-wrap:anywhere]"
      >
        {notes}
      </Streamdown>
    </section>
  );
}
