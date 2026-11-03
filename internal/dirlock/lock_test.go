package dirlock

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Run in a separate process so the contract also covers distinct processes,
// rather than depending on the OS's same-process lock behavior.
func TestLockProcess(t *testing.T) {
	root := os.Getenv("OTONEIGO_DIRLOCK_TEST_ROOT")
	if root == "" {
		return
	}
	unlock, err := Acquire(root)
	if errors.Is(err, ErrInUse) {
		os.Exit(23)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
}

func checkProcessOwner(t *testing.T, root string, wantBusy bool) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockProcess$")
	cmd.Env = append(os.Environ(), "OTONEIGO_DIRLOCK_TEST_ROOT="+root)
	output, err := cmd.CombinedOutput()
	if wantBusy {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 23 {
			t.Fatalf("second process: err=%v output=%s", err, output)
		}
	} else if err != nil {
		t.Fatalf("new process could not acquire: %v %s", err, output)
	}
}

func TestExclusiveOwnershipAndReacquire(t *testing.T) {
	root := t.TempDir()
	unlock, err := Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if unlock != nil {
			_ = unlock()
		}
	}()
	lockPath := filepath.Join(root, "cache.lock")
	before, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	checkProcessOwner(t, root, true)
	if second, err := Acquire(root); !errors.Is(err, ErrInUse) || second != nil {
		if second != nil {
			_ = second()
		}
		t.Fatalf("second owner: unlock=%v err=%v", second != nil, err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	unlock = nil
	after, err := os.Stat(lockPath)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("release must retain lock file: %v", err)
	}
	checkProcessOwner(t, root, false)
	unlock, err = Acquire(root)
	if err != nil {
		t.Fatalf("reacquire: %v", err)
	}
}

func TestInvalidLockPath(t *testing.T) {
	t.Run("directory", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "cache.lock"), 0700); err != nil {
			t.Fatal(err)
		}
		assertInvalidRoot(t, root)
	})
	t.Run("missing parent", func(t *testing.T) {
		assertInvalidRoot(t, filepath.Join(t.TempDir(), "missing", "root"))
	})
}

func assertInvalidRoot(t *testing.T, root string) {
	t.Helper()
	unlock, err := Acquire(root)
	if unlock != nil {
		_ = unlock()
		t.Fatal("invalid path acquired")
	}
	if err != errLock || errors.Is(err, ErrInUse) {
		t.Fatalf("invalid path must return generic failure: %v", err)
	}
}
