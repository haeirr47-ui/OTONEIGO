//go:build windows

package crashreport

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestRejectWindowsJunction(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "junction")
	// Directory junctions exercise Windows reparse points without depending on
	// the symbolic-link privilege or developer mode.
	if output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("junction creation unavailable: %v (%s)", err, output)
	}
	if _, err := Open(filepath.Join(link, "reports")); err != ErrIO {
		t.Fatal("junction ancestor accepted")
	}
	if _, err := os.Stat(filepath.Join(target, "reports")); !os.IsNotExist(err) {
		t.Fatal("Open created a directory through junction")
	}
	r := openRecorder(t)
	if err := os.Remove(r.dir); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", r.dir, target).CombinedOutput(); err != nil {
		t.Fatalf("replace with junction: %v (%s)", err, output)
	}
	if err := r.Record("replaced-directory", "secret"); err != ErrIO {
		t.Fatal("replaced junction directory accepted")
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		t.Fatal("Record wrote through junction")
	}
}

func TestLockedOldestReportBlocksAdmission(t *testing.T) {
	r := openRecorder(t)
	for i := 0; i < maxReports; i++ {
		if err := r.Record("fill-reports", nil); err != nil {
			t.Fatal(err)
		}
	}
	before := reports(t, r)
	oldest, err := windows.UTF16PtrFromString(before[0])
	if err != nil {
		t.Fatal(err)
	}
	// Excluding FILE_SHARE_DELETE reproduces a viewer/antivirus holding an old
	// report open while a fatal crash tries to reserve its replacement slot.
	handle, err := windows.CreateFile(oldest, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	for i := 0; i < 12; i++ {
		if err := r.Record("blocked-retention", "secret"); err != ErrIO {
			t.Fatalf("expected fixed retention error: %v", err)
		}
	}
	after := reports(t, r)
	if len(after) != maxReports {
		t.Fatalf("blocked retention accumulated files: %d", len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatal("blocked admission modified existing reports")
		}
	}
}
