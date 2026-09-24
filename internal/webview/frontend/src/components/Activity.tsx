import { useMemo, useState } from "react";
import type { ActivityBlock, Step } from "../types";
import { CodeBlock, DiffView } from "./CodeBlock";
import { Sparkle, Chevron, Check, X, Spinner, CopyIcon } from "./icons";

function fmtDuration(ms?: number) {
  if (!ms || ms <= 0) return "";
  if (ms < 1000) return ms + "ms";
  return (ms / 1000).toFixed(ms < 10000 ? 1 : 0) + "s";
}

function oneLine(s: string) {
  return (s || "").split("\n").map((x) => x.trim()).filter(Boolean)[0] ?? "";
}

function chipText(step: Step) {
  if (step.is_error) {
    return oneLine(step.result || "") || step.summary || "failed";
  }
  return step.summary || "";
}

function PlainOutput({ text, error }: { text: string; error?: boolean }) {
  const [copied, setCopied] = useState(false);
  const copy = () => {
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    });
  };
  return (
    <div
      className="overflow-hidden rounded-card bg-surface shadow-[0_0_0_1px_oklch(1_0_0/0.1)]"
      style={error ? { boxShadow: "0 0 0 1px color-mix(in srgb, var(--red) 35%, transparent)" } : undefined}
    >
      <div className="flex h-8 items-center justify-end border-b border-line px-2">
        <button
          type="button"
          onClick={copy}
          className="flex h-6 items-center gap-1.5 rounded-md px-2 text-[12px] text-ink-3 transition-colors hover:bg-hover hover:text-ink"
        >
          {copied ? <Check className="size-3 text-green" /> : <CopyIcon className="size-3" />}
          {copied ? "Copied" : "Copy"}
        </button>
      </div>
      <pre
        className="m-0 max-h-72 overflow-auto whitespace-pre-wrap break-words p-3 font-mono text-[12px] leading-[1.6]"
        style={{ color: error ? "var(--red)" : "var(--ink-2)" }}
      >
        {text}
      </pre>
    </div>
  );
}

function fmtTime(t?: string) {
  if (!t) return "";
  const d = new Date(t);
  if (isNaN(d.getTime())) return "";
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function ThinkingStep({ step, forceOpen }: { step: Step; forceOpen: boolean }) {
  const [open, setOpen] = useState(forceOpen);
  const shown = forceOpen || open;
  const lines = (step.text || "").split("\n").length;
  return (
    <div className={shown ? "" : ""}>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="group flex h-7 w-full items-center gap-2 rounded-control px-1.5 text-left transition-colors hover:bg-hover-2"
      >
        <Sparkle className="size-3.5 shrink-0 text-ink-3" />
        <span className="text-[12.5px] font-medium text-ink-2 group-hover:text-ink">Thinking</span>
        <span className="text-[11.5px] text-ink-3">{lines} lines</span>
        <Chevron className={`ml-auto size-3 text-ink-3 transition-transform duration-200 ${shown ? "rotate-180" : ""}`} />
      </button>
      <div className={`grid-collapse ${shown ? "open" : ""}`}>
        <div className="clip">
          <div className="mb-1 ml-[7px] border-l border-line py-0.5 pl-3.5">
            <p className="whitespace-pre-wrap text-[12.5px] leading-relaxed text-ink-2">{step.text}</p>
          </div>
        </div>
      </div>
    </div>
  );
}

function ToolStep({ step, forceOpen }: { step: Step; forceOpen: boolean }) {
  const [manual, setManual] = useState<boolean | null>(null);
  const open = manual ?? forceOpen;
  const hasBody = !!(step.args?.trim() || step.result?.trim() || step.diff?.length);
  const toggle = () => (hasBody ? setManual(!open) : undefined);
  return (
    <div className={step.is_error ? "" : ""}>
      <button
        type="button"
        onClick={toggle}
        className={`group flex h-7 w-full items-center gap-2 rounded-control px-1.5 text-left transition-colors ${hasBody ? "hover:bg-hover-2" : "cursor-default"}`}
      >
        <span className="flex size-4 shrink-0 items-center justify-center text-ink-3">
          {step.pending ? (
            <Spinner />
          ) : step.is_error ? (
            <X className="size-3.5 text-red" />
          ) : (
            <Check className="size-3.5 text-green" />
          )}
        </span>
        <span className="shrink-0 text-[12.5px] font-medium text-ink">{step.name}</span>
        <span className={`inline-flex h-[22px] min-w-0 flex-1 items-center truncate rounded-chip bg-field px-1.5 font-mono text-[11.5px] text-ink-2 shadow-[0_0_0_1px_var(--line)] ${step.is_error ? "text-red" : ""}`}>
          {chipText(step)}
        </span>
        <span className="ml-auto flex shrink-0 items-center gap-1.5 font-mono text-[11px] text-ink-3">
          {step.pending ? (
            <span className="text-orange">running</span>
          ) : (
            <>
              <span className={step.is_error ? "text-red" : "text-green"}>{step.is_error ? "failed" : "done"}</span>
              {fmtDuration(step.duration_ms) && <span>{fmtDuration(step.duration_ms)}</span>}
            </>
          )}
        </span>
        {hasBody && <Chevron className={`size-3 shrink-0 text-ink-3 transition-transform duration-200 ${open ? "rotate-180" : ""}`} />}
      </button>
      <div className={`grid-collapse ${open && hasBody ? "open" : ""}`}>
        <div className="clip">
          <div className="mb-1 ml-[7px] flex flex-col gap-2 border-l border-line py-1 pl-3.5">
            {step.args?.trim() && (
              <>
                <span className="text-[10.5px] font-semibold uppercase tracking-[0.08em] text-ink-3">Arguments</span>
                <CodeBlock lang="json" code={step.args} />
              </>
            )}
            {step.diff && step.diff.length > 0 && (
              <>
                <span className="text-[10.5px] font-semibold uppercase tracking-[0.08em] text-ink-3">Changes</span>
                <DiffView lines={step.diff} />
              </>
            )}
            {step.result?.trim() && (
              <>
                <span className="text-[10.5px] font-semibold uppercase tracking-[0.08em] text-ink-3">Result</span>
                <PlainOutput text={step.result} error={!!step.is_error} />
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

export function Activity({ block }: { block: ActivityBlock }) {
  const running = !block.settled;
  const [expanded, setExpanded] = useState(false);
  const open = running || expanded;

  const elapsed = useMemo(() => {
    if (!block.end_time || block.start_time >= block.end_time) return "";
    return fmtDuration(new Date(block.end_time).getTime() - new Date(block.start_time).getTime());
  }, [block.start_time, block.end_time]);

  const toolChips = (block.tools ?? [])
    .slice()
    .sort((a, b) => b.count - a.count)
    .map((t) => (t.count > 1 ? `${t.name} ×${t.count}` : t.name));

  return (
    <div className="min-w-0">
      <button
        type="button"
        onClick={() => !running && setExpanded((v) => !v)}
        className={`flex w-fit max-w-full items-center gap-1.5 rounded-control px-1.5 py-1 text-[12.5px] ${running ? "cursor-default" : "text-ink-2 hover:bg-hover-2"}`}
      >
        <Chevron className={`size-3 transition-transform duration-200 ${open ? "rotate-0" : "-rotate-90"}`} />
        {running ? (
          <>
            <Sparkle className="size-3.5 text-ink-3" />
            <span className="shimmer-text font-medium">Working…</span>
          </>
        ) : (
          <>
            {block.thinking_lines > 0 && (
              <span className="shrink-0">
                Thought <span className="text-ink-3">{block.thinking_lines} lines</span>
              </span>
            )}
            {block.thinking_lines > 0 && block.tool_count > 0 && <span className="text-ink-3">·</span>}
            {block.tool_count > 0 && (
              <span className="shrink-0">
                {block.tool_count} {block.tool_count === 1 ? "tool" : "tools"}
              </span>
            )}
            {toolChips.map((c) => (
              <span key={c} className="hidden truncate font-mono text-[11.5px] text-ink-3 sm:inline">
                · {c}
              </span>
            ))}
            {elapsed && <span className="ml-1 shrink-0 font-mono text-[11px] text-ink-3">{elapsed}</span>}
          </>
        )}
      </button>

      <div className={`grid-collapse ${open ? "open" : ""}`}>
        <div className="clip">
          <div className="mt-1 flex flex-col gap-0.5 pb-1 pl-0.5">
            {(block.steps ?? []).map((step, i) =>
              step.kind === "thinking" ? (
                <ThinkingStep key={i} step={step} forceOpen={running} />
              ) : (
                <ToolStep key={i} step={step} forceOpen={running && !!step.pending} />
              )
            )}
          </div>
        </div>
      </div>

      {block.settled && ((block.in_tokens ?? 0) > 0 || (block.out_tokens ?? 0) > 0) && (
        <div className="mt-0.5 pl-1.5 font-mono text-[10.5px] text-ink-3">
          ↑{block.in_tokens ?? 0} ↓{block.out_tokens ?? 0} tokens · {fmtTime(block.end_time)}
        </div>
      )}
    </div>
  );
}
