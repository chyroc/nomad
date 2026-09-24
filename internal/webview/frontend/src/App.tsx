import { useEffect, useRef, useState } from "react";
import type { SessionData, SessionList } from "./types";
import { TopBar, type LiveState } from "./components/TopBar";
import { Message } from "./components/Message";
import { Activity } from "./components/Activity";
import { SessionListView } from "./components/SessionListView";
import { useTheme } from "./useTheme";

const POLL_MS = 1500;

function route(): { name: "list" } | { name: "session"; id: string } {
  const m = window.location.pathname.match(/^\/sessions\/([^/]+)\/?$/);
  return m ? { name: "session", id: decodeURIComponent(m[1]) } : { name: "list" };
}

function fmtClock(t: string) {
  if (!t) return "";
  const d = new Date(t);
  return isNaN(d.getTime()) ? "" : d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function nearBottom() {
  return window.innerHeight + window.scrollY >= document.body.offsetHeight - 140;
}

export default function App() {
  const [view] = useState(route);
  const [sessionData, setSessionData] = useState<SessionData | null>(null);
  const [listData, setListData] = useState<SessionList | null>(null);
  const [live, setLive] = useState<LiveState>("");
  const [theme, toggleTheme] = useTheme();
  const errorsRef = useRef(0);
  const countRef = useRef(0);

  const endpoint = view.name === "session" ? `/api/sessions/${encodeURIComponent(view.id)}` : "/api/sessions";

  useEffect(() => {
    let alive = true;
    errorsRef.current = 0;
    countRef.current = 0;
    const tick = () => {
      fetch(endpoint, { cache: "no-store" })
        .then((r) => {
          if (!r.ok) throw new Error("HTTP " + r.status);
          errorsRef.current = 0;
          if (alive) setLive("");
          return r.json();
        })
        .then((d) => {
          if (!alive) return;
          if (view.name === "session") {
            const sd = d as SessionData;
            const stick = nearBottom();
            const grew = (sd.blocks?.length ?? 0) > countRef.current;
            countRef.current = sd.blocks?.length ?? 0;
            setSessionData(sd);
            if (stick && grew) requestAnimationFrame(() => window.scrollTo({ top: document.body.scrollHeight }));
          } else {
            setListData(d as SessionList);
          }
        })
        .catch(() => {
          if (!alive) return;
          errorsRef.current += 1;
          setLive(errorsRef.current >= 3 ? "dead" : "stale");
        });
    };
    tick();
    const id = setInterval(tick, POLL_MS);
    return () => {
      alive = false;
      clearInterval(id);
    };
  }, [endpoint, view.name]);

  const openSession = (id: string) => {
    window.history.pushState({}, "", "/sessions/" + encodeURIComponent(id));
    window.dispatchEvent(new PopStateEvent("popstate"));
  };
  useEffect(() => {
    const onPop = () => window.location.reload();
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);

  const title =
    view.name === "session" ? sessionData?.title ?? view.id : "All sessions";
  const workspace = view.name === "session" ? sessionData?.workspace ?? "" : listData?.workspace ?? "";
  const sessionId = view.name === "session" ? view.id : "";
  const updatedAt = fmtClock(view.name === "session" ? sessionData?.updated_at ?? "" : "");

  return (
    <>
      <TopBar
        title={title}
        sessionId={sessionId}
        workspace={workspace}
        updatedAt={updatedAt}
        live={live}
        theme={theme}
        onToggleTheme={toggleTheme}
        onHome={view.name === "session" ? () => (window.location.href = "/") : undefined}
      />
      <main className="mx-auto max-w-[820px] px-5 py-7 pb-32 sm:px-8">
        {view.name === "list" ? (
          <SessionListView sessions={listData?.sessions ?? []} onOpen={openSession} />
        ) : (
          <SessionView data={sessionData} />
        )}
      </main>
    </>
  );
}

function SessionView({ data }: { data: SessionData | null }) {
  const blocks = data?.blocks ?? [];
  const hasContent = !!blocks.length;
  if (!data) {
    return <div className="py-16 text-center text-[13px] text-ink-3">Loading…</div>;
  }
  if (!hasContent) {
    return (
      <div className="mx-auto mt-[12vh] max-w-[460px] rounded-window p-11 text-center shadow-[0_0_0_1px_var(--line)]">
        <div className="mb-1 text-[13.5px] font-semibold text-ink-2">No messages yet</div>
        <div className="text-[12.5px] text-ink-3">This view updates as the conversation runs.</div>
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-4">
      {blocks.map((b, i) => {
        if (b.type === "message") return <Message key={i} block={b} />;
        if (b.type === "activity")
          return (
            <div key={i} className="pl-0.5 animate-fade-in">
              <Activity block={b} />
            </div>
          );
        return (
          <div key={i} className="rounded-control p-3 text-[13px] text-red animate-fade-up" style={{ background: "var(--red-tint)" }}>
            {b.text}
          </div>
        );
      })}
    </div>
  );
}
