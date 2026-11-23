package agent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const stateLockFile = "agent.lock"

var errStateLocked = errors.New("state directory is already locked by another tailfile agent")

var (
	inprocLocksMu sync.Mutex
	inprocLocks   = map[string]bool{}
)

type stateLock struct {
	key  string
	f    *os.File
	once sync.Once
}

func acquireStateLock(stateDir string) (io.Closer, error) {
	if stateDir == "" {
		return nopCloser{}, nil
	}
	abs, err := filepath.Abs(stateDir)
	if err != nil {
		abs = filepath.Clean(stateDir)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(abs, 0o700); err != nil {
		return nil, err
	}

	inprocLocksMu.Lock()
	if inprocLocks[abs] {
		inprocLocksMu.Unlock()
		return nil, fmt.Errorf("%w (%s)", errStateLocked, abs)
	}
	inprocLocks[abs] = true
	inprocLocksMu.Unlock()

	cleanupInproc := func() {
		inprocLocksMu.Lock()
		delete(inprocLocks, abs)
		inprocLocksMu.Unlock()
	}

	lockPath := filepath.Join(abs, stateLockFile)
	f, err := openAndLockFile(lockPath)
	if err != nil {
		cleanupInproc()
		return nil, fmt.Errorf("%w (%s): %v", errStateLocked, abs, err)
	}
	return &stateLock{key: abs, f: f}, nil
}

func (l *stateLock) Close() error {
	var err error
	l.once.Do(func() {
		if l.f != nil {
			err = unlockAndCloseFile(l.f)
			l.f = nil
		}
		inprocLocksMu.Lock()
		delete(inprocLocks, l.key)
		inprocLocksMu.Unlock()
	})
	return err
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }
