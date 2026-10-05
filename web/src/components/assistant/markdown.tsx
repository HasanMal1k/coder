import type { ComponentProps } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { cn } from "@/lib/utils";

type Components = NonNullable<ComponentProps<typeof ReactMarkdown>["components"]>;

// The model writes Markdown. Each element is styled here, with the app's own
// tokens, so its messages look like part of the page in both themes.
const COMPONENTS: Components = {
  p: (props) => <p className="leading-relaxed [&:not(:first-child)]:mt-3" {...props} />,
  a: (props) => (
    <a
      className="font-medium text-primary underline underline-offset-4 hover:no-underline"
      target="_blank"
      rel="noreferrer"
      {...props}
    />
  ),
  ul: (props) => (
    <ul className="my-3 list-disc space-y-1 pl-5 marker:text-muted-foreground" {...props} />
  ),
  ol: (props) => (
    <ol className="my-3 list-decimal space-y-1 pl-5 marker:text-muted-foreground" {...props} />
  ),
  li: (props) => <li className="leading-relaxed" {...props} />,
  h1: (props) => <h3 className="mt-5 mb-2 text-base font-semibold first:mt-0" {...props} />,
  h2: (props) => <h3 className="mt-5 mb-2 text-base font-semibold first:mt-0" {...props} />,
  h3: (props) => <h4 className="mt-4 mb-2 text-sm font-semibold first:mt-0" {...props} />,
  h4: (props) => <h4 className="mt-4 mb-2 text-sm font-semibold first:mt-0" {...props} />,
  blockquote: (props) => (
    <blockquote className="my-3 border-l-2 pl-4 text-muted-foreground" {...props} />
  ),
  hr: () => <hr className="my-4" />,
  // A block of code arrives as <pre><code>; a word of code as <code> alone.
  // The block's own styling undoes the inline look.
  pre: (props) => (
    <pre
      className="my-3 overflow-x-auto rounded-md border bg-muted/50 p-3 font-mono text-[13px] leading-relaxed [&_code]:rounded-none [&_code]:bg-transparent [&_code]:p-0"
      {...props}
    />
  ),
  code: (props) => (
    <code className="rounded-md bg-muted px-1.5 py-0.5 font-mono text-[0.9em]" {...props} />
  ),
  table: (props) => (
    <div className="my-3 overflow-x-auto rounded-md border">
      <table className="w-full text-left text-sm" {...props} />
    </div>
  ),
  th: (props) => <th className="border-b bg-muted/50 px-3 py-2 font-medium" {...props} />,
  td: (props) => <td className="border-b px-3 py-2 last:border-b-0" {...props} />,
};

/** Text written by a model, rendered as the Markdown it is. */
export function Markdown({ children, className }: { children: string; className?: string }) {
  return (
    <div className={cn("min-w-0 text-sm wrap-anywhere", className)}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={COMPONENTS}>
        {children}
      </ReactMarkdown>
    </div>
  );
}
