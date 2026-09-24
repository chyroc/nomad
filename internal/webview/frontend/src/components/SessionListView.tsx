import type { SessionSummary } from "../types";

function fmt(t: string) {
  if (!t) return "";
  const d = new Date(t);
  if (isNaN(d.getTime())) return "";
  return d.toLocaleString([], { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

export function SessionListView({
  sessions,
  onOpen,
}: {
  sessions: SessionSummary[];
  onOpen: (id: string) => void;
}) {
  if (sessions.length === 0) {
    return (
      <div className="mx-auto mt-[14vh] max-w-[460px] rounded-window p-10 text-center shadow-[0_0_0_1px_var(--line)]">
        <div className="mb-2 text-[14px] font-semibold text-ink-2">No sessions yet</div>
        <div className="text-[12.5px] text-ink-3">Start a conversation in nomad, it will appear here automatically.</div>
      </div>
    );
  }
  return (
    <div className="mx-auto max-w-[720px] pt-2">
      <h1 className="mb-3 px-1 text-[13px] font-semibold text-ink-2">Sessions · {sessions.length}</h1>
      <div className="flex flex-col gap-1">
        {sessions.map((s) => (
          <button
            key={s.id}
            type="button"
            onClick={() => onOpen(s.id)}
            className="group flex items-center gap-3 rounded-control px-3 py-2.5 text-left transition-colors hover:bg-hover-2"
          >
            <span className="flex size-7 shrink-0 items-center justify-center rounded-[8px] bg-field text-ink-3 shadow-[0_0_0_1px_var(--line)]">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
                <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />
              </svg>
            </span>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-[13.5px] font-medium text-ink group-hover:text-ink">{s.title || s.id}</span>
              <span className="block truncate font-mono text-[11px] text-ink-3">{s.id}</span>
            </span>
            <span className="shrink-0 font-mono text-[11px] text-ink-3">{fmt(s.updated_at)}</span>
          </button>
        ))}
      </div>
    </div>
  );
}
