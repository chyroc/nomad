export type LiveState = "" | "stale" | "dead";

export function TopBar({
  title,
  sessionId,
  workspace,
  updatedAt,
  live,
  theme,
  onToggleTheme,
  onHome,
}: {
  title: string;
  sessionId: string;
  workspace: string;
  updatedAt: string;
  live: LiveState;
  theme: "dark" | "light";
  onToggleTheme: () => void;
  onHome?: () => void;
}) {
  const label = live === "dead" ? "offline" : live === "stale" ? "reconnecting" : "live";
  return (
    <header className="sticky top-0 z-20 border-b border-line bg-page/85 backdrop-blur">
      <div className="mx-auto flex h-[60px] max-w-[900px] items-center justify-between gap-4 px-5">
        <div className="flex min-w-0 items-center gap-3">
          <span className="flex size-[30px] shrink-0 items-center justify-center rounded-[9px] bg-ink text-page">
            <svg viewBox="0 0 24 24" fill="currentColor" className="size-3.5" aria-hidden>
              <path d="m12 2 2.5 7.5L22 12l-7.5 2.5L12 22l-2.5-7.5L2 12l7.5-2.5L12 2Z" />
            </svg>
          </span>
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="truncate text-[14px] font-semibold">{title || "Session"}</span>
              <span
                className={`inline-flex h-[19px] shrink-0 items-center gap-1.5 rounded-full px-2 text-[10.5px] font-medium ${
                  live === "dead" ? "bg-red-tint text-red" : live === "stale" ? "bg-orange-tint text-orange" : "bg-green-tint text-green"
                }`}
              >
                <span className="size-1.5 rounded-full bg-current" />
                {label}
              </span>
            </div>
            <div className="mt-px truncate text-[11.5px] text-ink-3">
              {sessionId && (
                <>
                  <span className="font-mono">{sessionId}</span>
                  {workspace && <span className="mx-1.5 opacity-50">·</span>}
                </>
              )}
              {workspace && <span>{workspace}</span>}
            </div>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {onHome && (
            <button
              type="button"
              onClick={onHome}
              aria-label="All sessions"
              title="All sessions"
              className="flex size-8 items-center justify-center rounded-control text-ink-3 transition-colors hover:bg-hover-2 hover:text-ink"
            >
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
                <path d="m12 19-7-7 7-7M19 12H5" />
              </svg>
            </button>
          )}
          <div className="hidden font-mono text-[11.5px] text-ink-3 sm:block">{updatedAt}</div>
          <button
            type="button"
            onClick={onToggleTheme}
            aria-label={`Switch to ${theme === "dark" ? "light" : "dark"} mode`}
            title={`Switch to ${theme === "dark" ? "light" : "dark"} mode`}
            className="flex size-8 items-center justify-center rounded-control text-ink-3 transition-colors hover:bg-hover-2 hover:text-ink"
          >
            {theme === "dark" ? (
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
                <circle cx="12" cy="12" r="4" />
                <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
              </svg>
            ) : (
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
                <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" />
              </svg>
            )}
          </button>
        </div>
      </div>
    </header>
  );
}
