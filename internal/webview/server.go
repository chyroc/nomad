// Package webview serves a single-page visualization of the local
// session transcripts over loopback or every interface. The page lists
// every session and renders one at /sessions/<id>, polling the JSON
// endpoint for live updates.
package webview

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chyroc/nomad/internal/loop"
	"github.com/chyroc/nomad/internal/store"
)

//go:embed all:dist
var distFS embed.FS

// SessionStore loads and lists the persisted transcripts.
type SessionStore interface {
	Load(id string) ([]loop.Event, error)
	List() ([]store.SessionRecord, error)
}

const (
	HostLoopback = "127.0.0.1"
	HostAll      = "0.0.0.0"

	// DefaultPort is the fixed web-view port. When it is already in
	// use the server probes the next few ports instead.
	DefaultPort = 4173
	maxPortScan = 32
)

// ParseHost resolves the host enum: "loopback" (default) binds the
// local interface only, "all" binds every interface. It is idempotent:
// the resolved constants 127.0.0.1 / 0.0.0.0 pass through unchanged.
func ParseHost(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", "loopback", HostLoopback:
		return HostLoopback, nil
	case "all", HostAll:
		return HostAll, nil
	default:
		return "", fmt.Errorf("invalid host %q (allowed: loopback, all)", value)
	}
}

// Server hosts the transcript browser on a local interface.
type Server struct {
	store     SessionStore
	workspace string
	host      string
	port      int

	ln   net.Listener
	srv  *http.Server
	done chan struct{}
}

// New prepares a server bound to host on DefaultPort. Resolve an enum
// with ParseHost first.
func New(source SessionStore, workspace, host string) (*Server, error) {
	return NewOnPort(source, workspace, host, DefaultPort)
}

// NewOnPort prepares a server bound to host:port; a port of 0 asks the
// OS for an ephemeral port.
func NewOnPort(source SessionStore, workspace, host string, port int) (*Server, error) {
	if net.ParseIP(host) == nil {
		return nil, fmt.Errorf("invalid bind host %q", host)
	}
	s := &Server{store: source, workspace: workspace, host: host, port: port}
	s.srv = &http.Server{Handler: s.buildMux(), ReadHeaderTimeout: 5 * time.Second}
	return s, nil
}

func (s *Server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions", s.handleList)
	mux.HandleFunc("GET /api/sessions/{id}", s.handleSession)
	mux.Handle("GET /", s.staticHandler())
	return mux
}

// Handler returns the HTTP handler, mainly for tests.
func (s *Server) Handler() http.Handler { return s.buildMux() }

// Start binds the configured host. It prefers the fixed port and, if
// that is taken, probes the next ports before falling back to an
// ephemeral one. It then serves in the background.
func (s *Server) Start() error {
	ln, err := s.listen()
	if err != nil {
		return err
	}
	s.ln = ln
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		_ = s.srv.Serve(ln)
	}()
	return nil
}

func (s *Server) listen() (net.Listener, error) {
	if s.port == 0 {
		return net.Listen("tcp", net.JoinHostPort(s.host, "0"))
	}
	for p := s.port; p < s.port+maxPortScan; p++ {
		if ln, err := net.Listen("tcp", net.JoinHostPort(s.host, strconv.Itoa(p))); err == nil {
			return ln, nil
		}
	}
	return net.Listen("tcp", net.JoinHostPort(s.host, "0"))
}

func (s *Server) Host() string { return s.host }
func (s *Server) Port() int    { return s.ln.Addr().(*net.TCPAddr).Port }
func (s *Server) Addr() string { return s.ln.Addr().String() }
func (s *Server) URLFor(host string) string {
	return "http://" + net.JoinHostPort(host, strconv.Itoa(s.Port()))
}
func (s *Server) URL() string         { return s.URLFor(s.host) }
func (s *Server) LoopbackURL() string { return s.URLFor(HostLoopback) }

// SessionURL returns the page address for one transcript.
func (s *Server) SessionURL(host, id string) string {
	return s.URLFor(host) + "sessions/" + id
}

// LoopbackSessionURL is the 127.0.0.1 address for one transcript.
func (s *Server) LoopbackSessionURL(id string) string {
	return s.SessionURL(HostLoopback, id)
}

// LANURLs returns the single externally reachable base URL when bound
// to 0.0.0.0 (the default-route IPv4, skipping virtual interfaces).
func (s *Server) LANURLs() []string {
	if s.host != HostAll {
		return nil
	}
	if ip := externalIPv4(); ip != "" {
		return []string{s.URLFor(ip)}
	}
	return nil
}

var virtualIfacePrefixes = []string{
	"lo", "docker", "br-", "virbr", "veth", "cni", "flannel",
	"kube", "tun", "tap", "dummy", "tailscale",
}

func externalIPv4() string {
	if conn, err := net.Dial("udp", "1.1.1.1:80"); err == nil {
		addr, ok := conn.LocalAddr().(*net.UDPAddr)
		_ = conn.Close()
		if ok && addr.IP.To4() != nil && !addr.IP.IsLoopback() {
			return addr.IP.String()
		}
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || isVirtualIface(ifc.Name) {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ip4 := ipnet.IP.To4(); ip4 != nil && ip4.IsGlobalUnicast() && !ip4.IsLoopback() {
				return ip4.String()
			}
		}
	}
	return ""
}

func isVirtualIface(name string) bool {
	for _, prefix := range virtualIfacePrefixes {
		if name == prefix || strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// ExternalIPv4 exposes the default-route source address for clients
// that only know the daemon port.
func ExternalIPv4() string { return externalIPv4() }

// Close stops accepting and waits for in-flight requests to finish.
func (s *Server) Close() error {
	if s.srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := s.srv.Shutdown(ctx)
	if s.done != nil {
		<-s.done
	}
	return err
}

// SessionSummary is one row in the session list.
type SessionSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SessionList is the GET /api/sessions payload.
type SessionList struct {
	Workspace string           `json:"workspace"`
	Sessions  []SessionSummary `json:"sessions"`
}

func (s *Server) handleList(w http.ResponseWriter, _ *http.Request) {
	out := SessionList{Workspace: s.workspace, Sessions: []SessionSummary{}}
	records, err := s.store.List()
	if err != nil {
		writeJSON(w, out)
		return
	}
	for _, r := range records {
		out.Sessions = append(out.Sessions, SessionSummary{ID: r.ID, Title: r.Title, UpdatedAt: r.UpdatedAt})
	}
	writeJSON(w, out)
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	events, err := s.store.Load(id)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	writeJSON(w, buildSessionData(id, s.workspace, events))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) staticHandler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return unavailable()
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/" {
			serveIndex(sub, w)
			return
		}
		if _, err := fs.Stat(sub, strings.TrimPrefix(r.URL.Path, "/")); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/sessions/") {
			serveIndex(sub, w)
			return
		}
		http.NotFound(w, r)
	})
}

func serveIndex(sub fs.FS, w http.ResponseWriter) {
	if data, err := fs.ReadFile(sub, "index.html"); err == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = fmt.Fprint(w, buildNotice)
}

func unavailable() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprint(w, buildNotice)
	})
}

const buildNotice = `<!doctype html><meta charset="utf-8"><title>web view</title>
<body style="font-family:system-ui;background:#0b0b0d;color:#ececf1;padding:48px;max-width:560px">
<h3 style="margin:0 0 8px">Frontend not built</h3>
<p style="color:#9b9ba6;font-size:14px">Run <code style="background:#16161a;padding:2px 6px;border-radius:4px">make web</code> (or ./dev.sh) to produce the embedded web view, then restart.</p>
</body>`
