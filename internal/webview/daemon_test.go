package webview

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDaemonSingleton(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "web.lock")
	state := filepath.Join(dir, "web.json")

	d1 := NewDaemon(dir)
	if err := d1.Acquire(); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	st := DaemonState{PID: os.Getpid(), Port: 43210, Host: HostLoopback}
	if err := d1.Publish(st); err != nil {
		t.Fatal(err)
	}

	d2 := NewDaemon(dir)
	if err := d2.Acquire(); err != ErrAlreadyRunning {
		t.Fatalf("second acquire = %v, want ErrAlreadyRunning", err)
	}

	got, ok := d2.Running()
	if !ok || got.Port != 43210 || got.Host != HostLoopback {
		t.Fatalf("running state wrong: %+v ok=%v", got, ok)
	}

	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("lock file missing: %v", err)
	}
	if _, err := os.Stat(state); err != nil {
		t.Fatalf("state file missing: %v", err)
	}
}

func TestDaemonRunningFalseWithoutState(t *testing.T) {
	d := NewDaemon(t.TempDir())
	if _, ok := d.Running(); ok {
		t.Fatal("empty dir must report no daemon")
	}
}
