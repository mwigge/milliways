package daemon

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"syscall"
)

// Lock is an exclusive flock on a pid file. Per milliwaysd/spec.md, the
// daemon SHALL acquire it before binding the UDS, with stale-lock takeover
// when the recorded pid is no longer running.
type Lock struct {
	file *os.File
	path string
}

// AcquireLock obtains an exclusive flock on pidPath. If the lock is held by a
// process that is already gone (kill -0 fails), the stale lock is taken over
// with a logged warning. Returns an error if the lock cannot be acquired
// after one stale-lock retry.
func AcquireLock(pidPath string) (*Lock, error) {
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(pidPath, os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			return nil, fmt.Errorf("open pid: %w", err)
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			if !errors.Is(err, syscall.EWOULDBLOCK) {
				f.Close()
				return nil, fmt.Errorf("flock: %w", err)
			}
			// Already locked — check if stale.
			if attempt == 0 && isStaleLock(f) {
				slog.Warn("stale lock detected, taking over", "pid_file", pidPath)
				f.Close()
				continue
			}
			pid, _ := readPid(f)
			f.Close()
			return nil, fmt.Errorf("daemon already running (pid %d)", pid)
		}
		// Lock acquired — write our pid.
		f.Truncate(0)
		f.Seek(0, 0)
		if _, err := f.WriteString(strconv.Itoa(os.Getpid()) + "\n"); err != nil {
			f.Close()
			return nil, fmt.Errorf("write pid: %w", err)
		}
		f.Sync()
		return &Lock{file: f, path: pidPath}, nil
	}
	return nil, fmt.Errorf("could not acquire lock after stale-lock retry")
}

func readPid(f *os.File) (int, error) {
	f.Seek(0, 0)
	var pid int
	_, err := fmt.Fscanln(f, &pid)
	return pid, err
}

func isStaleLock(f *os.File) bool {
	pid, err := readPid(f)
	if err != nil || pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) != nil
}

// Release the flock and remove the pid file. Safe to call on a nil receiver.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	l.file.Close()
	return os.Remove(l.path)
}
