package webview

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// DaemonState is the on-disk descriptor of the running singleton.
type DaemonState struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	Host      string    `json:"host"`
	StartedAt time.Time `json:"started_at"`
}

// Daemon serializes web-server startup machine-wide. The server
// process holds a flock for its whole lifetime and publishes its port
// in a state file; client processes only read that file.
type Daemon struct {
	dir      string
	lockFile *os.File
}

func NewDaemon(dir string) *Daemon { return &Daemon{dir: dir} }

func (d *Daemon) statePath() string { return filepath.Join(d.dir, "web.json") }
func (d *Daemon) lockPath() string  { return filepath.Join(d.dir, "web.lock") }

var ErrAlreadyRunning = errors.New("web server already running")

// Acquire takes the exclusive lock. It fails with ErrAlreadyRunning if
// a live daemon already owns it. The lock is released automatically
// when the process exits.
func (d *Daemon) Acquire() error {
	if err := os.MkdirAll(d.dir, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(d.lockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return ErrAlreadyRunning
	}
	if st, ok := d.readAlive(); ok && processAlive(st.PID) && st.PID != os.Getpid() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
		return ErrAlreadyRunning
	}
	d.lockFile = lock
	return nil
}

// Publish writes this process's descriptor.
func (d *Daemon) Publish(st DaemonState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := d.statePath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, d.statePath())
}

// Adopt acquires the singleton lock and records the live endpoint.
func (d *Daemon) Adopt(host string, port int) (DaemonState, error) {
	if err := d.Acquire(); err != nil {
		return DaemonState{}, err
	}
	st := DaemonState{PID: os.Getpid(), Port: port, Host: host, StartedAt: time.Now()}
	if err := d.Publish(st); err != nil {
		return DaemonState{}, err
	}
	return st, nil
}

// RemoveState clears the descriptor on graceful shutdown.
func (d *Daemon) RemoveState() { _ = os.Remove(d.statePath()) }

// Running returns a live daemon's descriptor without locking.
func (d *Daemon) Running() (DaemonState, bool) {
	raw, err := os.ReadFile(d.statePath())
	if err != nil {
		return DaemonState{}, false
	}
	var st DaemonState
	if json.Unmarshal(raw, &st) != nil || st.PID == 0 || st.Port == 0 || !processAlive(st.PID) {
		_ = os.Remove(d.statePath())
		return DaemonState{}, false
	}
	return st, true
}

func (d *Daemon) readAlive() (DaemonState, bool) {
	raw, err := os.ReadFile(d.statePath())
	if err != nil {
		return DaemonState{}, false
	}
	var st DaemonState
	if json.Unmarshal(raw, &st) != nil || st.PID == 0 || st.Port == 0 {
		return DaemonState{}, false
	}
	return st, true
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if pid == os.Getpid() {
		return true
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return errors.Is(err, syscall.EPERM)
	}
	return true
}
