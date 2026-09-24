package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/chyroc/nomad/internal/webview"
)

func TestParseWebHost(t *testing.T) {
	cases := map[string]string{
		"": webview.HostLoopback, "  ": webview.HostLoopback,
		"--host loopback": webview.HostLoopback, "--host=loopback": webview.HostLoopback,
		"--host all": webview.HostAll, "--host=all": webview.HostAll,
		"--host 127.0.0.1": webview.HostLoopback, "--host 0.0.0.0": webview.HostAll,
	}
	for in, want := range cases {
		got, err := parseWebHost(in)
		if err != nil || got != want {
			t.Fatalf("parseWebHost(%q)=%q,%v want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"--host", "--bogus", "000", "all", "--host lan", "--host 1.2.3.4"} {
		if _, err := parseWebHost(bad); err == nil {
			t.Fatalf("parseWebHost(%q) must error", bad)
		}
	}
}

func TestOpenBrowserFallbackWithoutDisplay(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	if openBrowser(context.Background(), "http://127.0.0.1:0/") {
		t.Fatal("openBrowser must report false without a display")
	}
}

func TestSlashCommandNamesIncludesWeb(t *testing.T) {
	found := false
	for _, name := range slashCommandNames() {
		if name == "/web" {
			found = true
		}
	}
	if !found {
		t.Fatal("/web missing from completer")
	}
	if !strings.Contains(commandHelp(), "/web") {
		t.Fatal("/web missing from /help")
	}
}
