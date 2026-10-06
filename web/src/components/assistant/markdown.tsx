import { isValidElement, type ComponentProps, type ReactNode } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { cn } from "@/lib/utils";
import { CopyButton } from "../copy-button";

type Components = NonNullable<ComponentProps<typeof ReactMarkdown>["components"]>;

/** The text inside an element, however deeply it is nested. */
function textOf(node: ReactNode): string {
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(textOf).join("");
  if (isValidElement<{ children?: ReactNode }>(node)) return textOf(node.props.children);
  return "";
}

/**
 * A block of code: what language it says it is, a way to copy it, and the
 * code itself, which scrolls sideways sooner than wrap.
 */
function CodeBlock({ children }: { children?: ReactNode }) {
  // The block arrives as <pre><code class="language-…">.
  const code = isValidElement<{ className?: string }>(children) ? children.props : undefined;
  const language = code?.className?.match(/language-([\w+#.-]+)/)?.[1];
  const text = textOf(children).replace(/\n$/, "");

  return (
    <div className="my-3 overflow-hidden rounded-md border bg-card">
      <div className="flex h-8 items-center justify-between border-b pr-1 pl-3">
        <span className="font-mono text-xs text-muted-foreground" translate="no">
          {language ?? "text"}
        </span>
        <CopyButton value={text} label="Copy code" />
      </div>
      <pre
        // Reachable by keyboard, so a long line can be scrolled to without a mouse.
        tabIndex={0}
        className="overflow-x-auto p-3 font-mono text-[13px] leading-relaxed outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:ring-inset [&_code]:rounded-none [&_code]:border-0 [&_code]:bg-transparent [&_code]:p-0 [&_code]:text-[1em]"
      >
        {children}
      </pre>
    </div>
  );
}

// The model writes Markdown. Each element is styled here, with the app's own
// tokens, so its messages look like part of the page.
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
  h1: (props) => (
    <h3 className="mt-6 mb-2 text-base font-semibold tracking-tight first:mt-0" {...props} />
  ),
  h2: (props) => (
    <h3 className="mt-6 mb-2 text-base font-semibold tracking-tight first:mt-0" {...props} />
  ),
  h3: (props) => <h4 className="mt-5 mb-2 text-sm font-semibold first:mt-0" {...props} />,
  h4: (props) => <h4 className="mt-5 mb-2 text-sm font-semibold first:mt-0" {...props} />,
  blockquote: (props) => (
    <blockquote className="my-3 border-l-2 pl-4 text-muted-foreground" {...props} />
  ),
  hr: () => <hr className="my-4" />,
  // A block of code arrives as <pre><code>; a word of code as <code> alone.
  // The block's own styling undoes the inline look.
  pre: ({ children }) => <CodeBlock>{children}</CodeBlock>,
  code: (props) => (
    <code
      className="rounded-sm border bg-muted px-1 py-px font-mono text-[0.9em]"
      translate="no"
      {...props}
    />
  ),
  // A table keeps its words whole and scrolls sideways when the page is too
  // narrow for it, where the text around it would break a long word instead.
  table: (props) => (
    <div className="my-3 overflow-x-auto rounded-md border">
      <table className="w-full text-left text-sm wrap-normal tabular-nums" {...props} />
    </div>
  ),
  thead: (props) => <thead className="border-b bg-card" {...props} />,
  // The line under each row is the row's, so it runs the table's whole width.
  tr: (props) => <tr className="border-b last:border-b-0" {...props} />,
  th: (props) => (
    <th
      className="px-3 py-2 text-xs font-medium whitespace-nowrap text-muted-foreground"
      {...props}
    />
  ),
  td: (props) => <td className="px-3 py-2 align-top" {...props} />,
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
