package webview

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/nomad/internal/loop"
	"github.com/chyroc/nomad/internal/store"
)

func newTestStore(t *testing.T, id string) *store.SessionStore {
	t.Helper()
	s, err := store.NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	callArgs := `{"file_path":"a.txt","old_string":"hello\n","new_string":"hello\nworld\n"}`
	events := []loop.Event{
		{Kind: loop.EvUserMessage, Time: start, Content: "please add a line"},
		{Kind: loop.EvAssistantThinking, Time: start.Add(time.Second), Content: "the user wants another line"},
		{Kind: loop.EvToolCall, Time: start.Add(3 * time.Second), ToolCall: &loop.ToolCall{
			ID: "call-1", Name: "edit", Arguments: callArgs,
		}},
		{Kind: loop.EvToolResult, Time: start.Add(4500 * time.Millisecond), ToolName: "edit",
			ToolCall: &loop.ToolCall{ID: "call-1", Name: "edit"}, Result: "updated"},
		{Kind: loop.EvAssistantMessage, Time: start.Add(5 * time.Second), Content: "**done**"},
		{Kind: loop.EvTurnEnd, Time: start.Add(5500 * time.Millisecond), Usage: &loop.Usage{InputTokens: 11, OutputTokens: 7}},
	}
	for _, ev := range events {
		if err := s.Append(id, ev); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func newServer(t *testing.T, tr SessionStore) *Server {
	t.Helper()
	srv, err := New(tr, "/ws", HostLoopback)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestParseHost(t *testing.T) {
	cases := map[string]string{
		"": HostLoopback, "loopback": HostLoopback, "all": HostAll,
		"127.0.0.1": HostLoopback, "0.0.0.0": HostAll,
	}
	for in, want := range cases {
		got, err := ParseHost(in)
		if err != nil || got != want {
			t.Fatalf("ParseHost(%q)=%q,%v want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"127", "000", "0", "any", "1.2.3.4"} {
		if _, err := ParseHost(bad); err == nil {
			t.Fatalf("ParseHost(%q) must error", bad)
		}
	}
}

func TestStaticServesPageOrPlaceholder(t *testing.T) {
	ts := httptest.NewServer(newServer(t, nil).Handler())
	defer ts.Close()

	res, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("index status=%d", res.StatusCode)
	}
	if res.StatusCode == http.StatusOK && !strings.Contains(string(body), "<div id=\"root\">") {
		t.Fatal("built index missing root")
	}
	if res.StatusCode == http.StatusOK {
		js, err := http.Get(ts.URL + "/app.js")
		if err != nil {
			t.Fatal(err)
		}
		jb, _ := io.ReadAll(js.Body)
		js.Body.Close()
		if js.StatusCode != 200 || !strings.Contains(js.Header.Get("Content-Type"), "javascript") || len(jb) == 0 {
			t.Fatalf("app.js not served: %d %q %dB", js.StatusCode, js.Header.Get("Content-Type"), len(jb))
		}
	}

	spa, err := http.Get(ts.URL + "/sessions/abc123")
	if err != nil {
		t.Fatal(err)
	}
	spa.Body.Close()
	if spa.StatusCode != http.StatusOK && spa.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("SPA route status=%d", spa.StatusCode)
	}

	nf, err := http.Get(ts.URL + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	nf.Body.Close()
	if nf.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown path=%d want 404", nf.StatusCode)
	}
}

func TestListEndpoint(t *testing.T) {
	id := "sess-list-1"
	s := newTestStore(t, id)
	ts := httptest.NewServer(newServer(t, s).Handler())
	defer ts.Close()

	res, err := http.Get(ts.URL + "/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var list SessionList
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if list.Workspace != "/ws" || len(list.Sessions) != 1 || list.Sessions[0].ID != id {
		t.Fatalf("list wrong: %+v", list)
	}
	if list.Sessions[0].Title != "please add a line" {
		t.Fatalf("title=%q", list.Sessions[0].Title)
	}
}

func TestSessionEndpoint(t *testing.T) {
	id := "sess-one"
	s := newTestStore(t, id)
	ts := httptest.NewServer(newServer(t, s).Handler())
	defer ts.Close()

	res, err := http.Get(ts.URL + "/api/sessions/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status=%d", res.StatusCode)
	}
	var data SessionData
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		t.Fatal(err)
	}
	if data.SessionID != id {
		t.Fatalf("id=%q", data.SessionID)
	}
	var kinds []string
	var act *ActivityBlock
	for _, b := range data.Blocks {
		kinds = append(kinds, blockKind(b))
		if blockKind(b) == "activity" && act == nil {
			raw, _ := json.Marshal(b)
			a := &ActivityBlock{}
			_ = json.Unmarshal(raw, a)
			act = a
		}
	}
	if strings.Join(kinds, ",") != "message,activity,message" {
		t.Fatalf("kinds=%v", kinds)
	}
	if act == nil || !act.Settled || act.ToolCount != 1 || act.InTokens != 11 {
		t.Fatalf("activity wrong: %+v", act)
	}
	var tool *Step
	for _, st := range act.Steps {
		if st.Kind == "tool" {
			tool = st
		}
	}
	if tool == nil || tool.Pending || tool.Name != "edit" || len(tool.Diff) == 0 {
		t.Fatalf("tool wrong: %+v", tool)
	}

	missing, _ := http.Get(ts.URL + "/api/sessions/does-not-exist")
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing=%d want 404", missing.StatusCode)
	}
}

func blockKind(b Block) string { v, _ := b["type"].(string); return v }

func TestBuildSessionDataLiveUnsettled(t *testing.T) {
	now := time.Now()
	events := []loop.Event{
		{Kind: loop.EvUserMessage, Time: now, Content: "hi"},
		{Kind: loop.EvAssistantThinking, Time: now.Add(time.Second), Content: "a"},
		{Kind: loop.EvAssistantThinking, Time: now.Add(2 * time.Second), Content: "b"},
		{Kind: loop.EvToolCall, Time: now.Add(3 * time.Second), ToolCall: &loop.ToolCall{ID: "c", Name: "bash", Arguments: `{"command":"ls"}`}},
	}
	data := buildSessionData("s1", "/ws", events)
	var act ActivityBlock
	for _, b := range data.Blocks {
		if blockKind(b) == "activity" {
			raw, _ := json.Marshal(b)
			_ = json.Unmarshal(raw, &act)
		}
	}
	if act.Settled || act.ThinkingLines != 2 || act.ToolCount != 1 {
		t.Fatalf("live activity wrong: %+v", act)
	}
	if len(act.Steps) != 2 || !act.Steps[1].Pending {
		t.Fatalf("steps wrong: %+v", act.Steps)
	}
}

func TestToolResultMatchesByNameWhenCallIDMissing(t *testing.T) {
	now := time.Now()
	events := []loop.Event{
		{Kind: loop.EvUserMessage, Time: now, Content: "ls"},
		{Kind: loop.EvToolCall, Time: now.Add(time.Second), ToolCall: &loop.ToolCall{ID: "x", Name: "bash", Arguments: `{"command":"ls"}`}},
		{Kind: loop.EvToolResult, Time: now.Add(2 * time.Second), ToolName: "bash",
			ToolCall: &loop.ToolCall{ID: "", Name: "bash"}, Result: "exit_code: 0"},
	}
	data := buildSessionData("s1", "/ws", events)
	var act ActivityBlock
	for _, b := range data.Blocks {
		if blockKind(b) == "activity" {
			raw, _ := json.Marshal(b)
			_ = json.Unmarshal(raw, &act)
		}
	}
	if len(act.Steps) != 1 || act.Steps[0].Pending || act.Steps[0].Result != "exit_code: 0" {
		t.Fatalf("name-match wrong: %+v", act.Steps)
	}
}

func TestServerLifecycleAndBinds(t *testing.T) {
	srv, err := NewOnPort(newTestStore(t, "sess-life"), t.TempDir(), HostLoopback, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(srv.Addr(), "127.0.0.1:") {
		t.Fatalf("bound=%q", srv.Addr())
	}
	if _, err := http.Get("http://" + srv.Addr() + "/api/sessions"); err != nil {
		t.Fatal(err)
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := net.DialTimeout("tcp", srv.Addr(), 200*time.Millisecond); err == nil {
		t.Fatal("still accepting after Close")
	}

	all, err := NewOnPort(newTestStore(t, "sess-all"), t.TempDir(), HostAll, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := all.Start(); err != nil {
		t.Fatal(err)
	}
	defer all.Close()
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(all.Port()), time.Second)
	if err != nil {
		t.Fatalf("0.0.0.0 must accept IPv4: %v", err)
	}
	conn.Close()
	if urls := all.LANURLs(); len(urls) > 1 {
		t.Fatalf("at most one LAN url: %v", urls)
	}
}

func TestStartUsesFixedPortWhenFree(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	freePort := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()

	srv, err := NewOnPort(nil, t.TempDir(), HostLoopback, freePort)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if srv.Port() != freePort {
		t.Fatalf("port = %d, want fixed %d", srv.Port(), freePort)
	}
}

func TestStartScansWhenPortBusy(t *testing.T) {
	holder, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	base := holder.Addr().(*net.TCPAddr).Port

	srv, err := NewOnPort(nil, t.TempDir(), HostLoopback, base)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if srv.Port() == base {
		t.Fatalf("server reused the busy port %d", base)
	}
	if srv.Port() < base+1 || srv.Port() > base+maxPortScan {
		t.Fatalf("server did not scan past busy port: got %d from base %d", srv.Port(), base)
	}
}
