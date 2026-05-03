// Copyright 2024 The milliways Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package daemon

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"syscall"
	"time"
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
			// Already locked — check if stale (process gone) or superseded (binary newer).
			if attempt == 0 {
				if isStaleLock(f) {
					slog.Warn("stale lock detected, taking over", "pid_file", pidPath)
					f.Close()
					continue
				}
				if isSuperseded(f) {
					slog.Info("newer binary detected; replacing running daemon")
					f.Close()
					continue
				}
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

// isSuperseded returns true when the binary on disk is newer than the process
// that holds the lock started. In that case we SIGTERM the old process and wait
// briefly for it to exit so the new binary can take over.
func isSuperseded(f *os.File) bool {
	pid, err := readPid(f)
	if err != nil || pid <= 0 {
		return false
	}

	// Get mtime of the running binary on disk.
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	exeInfo, err := os.Stat(exe)
	if err != nil {
		return false
	}

	// Get the start time of the running process via /proc (Linux) or ps (macOS/BSD).
	procStart := processMtime(pid)
	if procStart.IsZero() {
		return false
	}

	if exeInfo.ModTime().After(procStart) {
		slog.Info("binary updated since daemon started; SIGTERMing old daemon",
			"old_pid", pid, "binary_mtime", exeInfo.ModTime(), "proc_start", procStart)
		proc, findErr := os.FindProcess(pid)
		if findErr == nil {
			_ = proc.Signal(syscall.SIGTERM)
			// Give the old daemon up to 3 seconds to exit cleanly.
			for i := 0; i < 30; i++ {
				time.Sleep(100 * time.Millisecond)
				if proc.Signal(syscall.Signal(0)) != nil {
					break // process gone
				}
			}
		}
		return true
	}
	return false
}

// processMtime returns an approximate start time for pid by reading the mtime
// of /proc/<pid> on Linux. Returns zero time on non-Linux or on error.
func processMtime(pid int) time.Time {
	info, err := os.Stat(fmt.Sprintf("/proc/%d", pid))
	if err != nil {
		return time.Time{} // macOS/BSD: /proc not available; fall back gracefully
	}
	return info.ModTime()
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
