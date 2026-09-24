import { useState } from "react";
import { CopyIcon, Check } from "./icons";
import type { DiffLine } from "../types";

export function CodeBlock({ lang, code }: { lang: string; code: string }) {
  const [copied, setCopied] = useState(false);
  const copy = () => {
    navigator.clipboard.writeText(code).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1400);
    });
  };
  return (
    <div className="overflow-hidden rounded-card bg-surface shadow-[0_0_0_1px_oklch(1_0_0/0.1)]">
      <div className="flex h-9 items-center gap-2 border-b border-line px-3 font-mono text-[12px]">
        <span className="text-ink-3">{lang}</span>
        <button
          type="button"
          onClick={copy}
          className="ml-auto flex h-6 items-center gap-1.5 rounded-md px-2 text-[12px] text-ink-3 transition-colors hover:bg-hover hover:text-ink"
        >
          {copied ? <Check className="size-3 text-green" /> : <CopyIcon className="size-3" />}
          {copied ? "Copied" : "Copy"}
        </button>
      </div>
      <pre className="m-0 overflow-x-auto p-3 font-mono text-[12.5px] leading-[1.65] text-ink-2">
        <code className="whitespace-pre">{code}</code>
      </pre>
    </div>
  );
}

const HATCH = "repeating-linear-gradient(45deg, var(--red) 0, var(--red) 1.5px, transparent 1.5px, transparent 3px)";

export function DiffView({ lines }: { lines: DiffLine[] }) {
  let oldNo = 0;
  let newNo = 0;
  return (
    <div className="overflow-hidden rounded-card bg-surface shadow-[0_0_0_1px_oklch(1_0_0/0.1)]">
      <div className="font-mono text-[12px] leading-[1.7]">
        {lines.map((l, i) => {
          if (l.kind === "@") {
            const m = l.text.match(/@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
            if (m) {
              oldNo = parseInt(m[1], 10) - 1;
              newNo = parseInt(m[2], 10) - 1;
            }
            return (
              <div key={i} className="bg-orange-tint text-orange">
                <code className="block whitespace-pre-wrap break-words px-3">{l.text}</code>
              </div>
            );
          }
          const add = l.kind === "+";
          const del = l.kind === "-";
          const num = del ? ++oldNo : add ? ++newNo : (++oldNo, ++newNo);
          return (
            <div
              key={i}
              className={`relative grid grid-cols-[26px_minmax(0,1fr)] items-start ${add ? "bg-green-tint" : del ? "bg-red-tint" : ""}`}
            >
              {(add || del) && (
                <span
                  className="pointer-events-none absolute inset-y-0 left-0 w-[3px]"
                  style={{ background: add ? "var(--green)" : HATCH }}
                />
              )}
              <span className={`select-none pr-1 text-center text-[11px] ${add ? "text-green" : del ? "text-red" : "text-ink-3"}`}>
                {num}
              </span>
              <code className="whitespace-pre-wrap break-words pl-1 pr-3 text-ink-2">
                <span className={`select-none ${add ? "text-green" : del ? "text-red" : ""}`}>{add ? "+" : del ? "−" : " "}</span>
                {l.text}
              </code>
            </div>
          );
        })}
      </div>
    </div>
  );
}
