package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/chyroc/nomad/internal/store"
	"github.com/chyroc/nomad/internal/webview"
)

func (a *App) webDaemon() *webview.Daemon {
	return webview.NewDaemon(a.paths.DataDir)
}

// RunWebServer is the detached daemon entrypoint: own the singleton
// lock, serve every transcript until the process is terminated.
func (a *App) RunWebServer() error {
	host, err := webview.ParseHost(a.opts.WebHost)
	if err != nil {
		return err
	}
	transcript, err := store.NewSessionStore(a.paths.SessionsDir())
	if err != nil {
		return err
	}
	srv, err := webview.New(transcript, a.paths.Workspace, host)
	if err != nil {
		return err
	}
	if err := srv.Start(); err != nil {
		return err
	}
	d := a.webDaemon()
	if _, err := d.Adopt(host, srv.Port()); err != nil {
		_ = srv.Close()
		return err
	}
	fmt.Printf("nomad web server listening on %s\n", srv.Addr())
	for _, u := range srv.LANURLs() {
		fmt.Printf("network %s\n", u)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	d.RemoveState()
	_ = srv.Close()
	return nil
}

// ensureWebDaemon makes sure exactly one detached web-server process
// is running bound to host (starting or restarting it if needed) and
// returns its state. Switching host stops the old daemon first so the
// machine still exposes only one port.
func (a *App) ensureWebDaemon() (webview.DaemonState, error) {
	host, err := webview.ParseHost(a.opts.WebHost)
	if err != nil {
		return webview.DaemonState{}, err
	}
	d := a.webDaemon()
	if st, ok := d.Running(); ok && st.Host == host {
		return st, nil
	}
	if st, ok := d.Running(); ok {
		_ = syscall.Kill(st.PID, syscall.SIGTERM)
		if err := waitDaemonGone(d, 3*time.Second); err != nil {
			return webview.DaemonState{}, err
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return webview.DaemonState{}, err
	}
	// Double-fork through a shell so the daemon is reparented to init
	// and reaped there on exit instead of becoming a zombie of the TUI.
	inner := strconv.Quote(exe) + " web-server --web-host " + host
	cmd := exec.Command("/bin/sh", "-c", `exec setsid sh -c '`+inner+` >/dev/null 2>&1 &'`)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return webview.DaemonState{}, fmt.Errorf("start web server: %w", err)
	}
	_ = cmd.Wait()

	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if st, ok := d.Running(); ok && st.Host == host {
			return st, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return webview.DaemonState{}, fmt.Errorf("web server did not become ready")
}

func waitDaemonGone(d *webview.Daemon, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, ok := d.Running(); !ok {
			return nil
		}
		time.Sleep(80 * time.Millisecond)
	}
	return fmt.Errorf("previous web server did not stop")
}

func daemonBaseURL(st webview.DaemonState) string {
	host := webview.HostLoopback
	return "http://" + host + ":" + fmt.Sprint(st.Port)
}

// cmdWeb ensures the singleton is running and opens the current
// session page.
func (a *App) cmdWeb(ctx context.Context) error {
	st, err := a.ensureWebDaemon()
	if err != nil {
		return err
	}
	base := daemonBaseURL(st)
	page := base + "/"
	if a.sessionID != "" {
		page = base + "/sessions/" + a.sessionID
	}
	a.printWebAddresses(st, page)
	if !openBrowser(ctx, page) {
		a.printf("%s(xdg-open unavailable — open the URL manually)%s\n", cDim, cReset)
	}
	return nil
}

func (a *App) printWebAddresses(st webview.DaemonState, page string) {
	a.printf("%sweb view (pid %d):%s\n", cBold, st.PID, cReset)
	a.printf("  %-8s%s\n", "local", page)
	if st.Host == webview.HostAll {
		if ip := webview.ExternalIPv4(); ip != "" {
			a.printf("  %-8shttp://%s:%d/\n", "network", ip, st.Port)
			if a.sessionID != "" {
				a.printf("  %-8shttp://%s:%d/sessions/%s\n", "network", ip, st.Port, a.sessionID)
			}
		}
		a.printf("%swarning: reachable on the network without authentication%s\n", cYellow, cReset)
	}
}

// maybeAutoStartWeb starts the singleton when --web is set, opening
// the current (possibly soon-to-exist) session page.
func (a *App) maybeAutoStartWeb(ctx context.Context) {
	if !a.opts.Web {
		return
	}
	_ = a.cmdWeb(ctx)
}

func parseWebHost(arg string) (string, error) {
	fields := strings.Fields(arg)
	switch len(fields) {
	case 0:
		return webview.HostLoopback, nil
	case 2:
		if fields[0] != "--host" {
			break
		}
		return webview.ParseHost(fields[1])
	case 1:
		if rest, ok := strings.CutPrefix(fields[0], "--host="); ok {
			return webview.ParseHost(rest)
		}
	}
	return "", fmt.Errorf("usage: /web [--host loopback|all]")
}

func openBrowser(ctx context.Context, url string) bool {
	if _, err := exec.LookPath("xdg-open"); err != nil {
		return false
	}
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}
	openCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return exec.CommandContext(openCtx, "xdg-open", url).Start() == nil
}
