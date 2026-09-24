import type { MessageBlock } from "../types";
import { Sparkle } from "./icons";

function fmtTime(t: string) {
  const d = new Date(t);
  return isNaN(d.getTime()) ? "" : d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

export function Message({ block }: { block: MessageBlock }) {
  if (block.role === "user") {
    return (
      <div className="flex justify-end pl-[15%] animate-fade-up">
        <div className="max-w-full">
          <div
            className="rounded-xl bg-field px-3.5 py-2 text-[13px] leading-relaxed shadow-[0_0_0_1px_var(--line)] prose"
            dangerouslySetInnerHTML={{ __html: block.html || "" }}
          />
          <div className="mt-1 text-right font-mono text-[10.5px] text-ink-3">{fmtTime(block.time)}</div>
        </div>
      </div>
    );
  }
  return (
    <div className="flex gap-3 animate-fade-up">
      <span className="mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-[9px] bg-ink text-page">
        <Sparkle className="size-3.5" />
      </span>
      <div className="min-w-0 flex-1">
        <span className="mb-1.5 block text-[11px] font-medium uppercase tracking-[0.09em] text-ink-3">
          Assistant · {fmtTime(block.time)}
        </span>
        <div className="prose" dangerouslySetInnerHTML={{ __html: block.html || "" }} />
      </div>
    </div>
  );
}
