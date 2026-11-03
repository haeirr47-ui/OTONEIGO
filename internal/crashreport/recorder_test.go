package crashreport

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type hostileValue struct{}

func (hostileValue) String() string               { panic("String called with private token") }
func (hostileValue) Error() string                { panic("Error called with private token") }
func (hostileValue) MarshalText() ([]byte, error) { panic("MarshalText called") }
func (hostileValue) MarshalJSON() ([]byte, error) { panic("MarshalJSON called") }

func openRecorder(t *testing.T) *Recorder {
	t.Helper()
	r, err := Open(filepath.Join(t.TempDir(), "reports"))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func reports(t *testing.T, r *Recorder) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(r.dir, "crash-*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestPrivacyAndBounds(t *testing.T) {
	r := openRecorder(t)
	secret := "https://private.example/audio?token=DO_NOT_PERSIST"
	values := []any{hostileValue{}, (*hostileValue)(nil), secret, strings.Repeat(secret, 100000), map[string]string{"token": secret}}
	for _, value := range values {
		if err := r.Record("test-crash", value); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range reports(t, r) {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) > maxReportBytes {
			t.Fatalf("oversized report: %d", len(data))
		}
		for _, forbidden := range []string{secret, "DO_NOT_PERSIST", "hostileValue", "private token", r.dir, "goroutine "} {
			if bytes.Contains(data, []byte(forbidden)) {
				t.Fatalf("private data in report: %s", forbidden)
			}
		}
		if !bytes.Contains(data, []byte("panic_value: [redacted]")) || !bytes.Contains(data, []byte("recorder_test.go:")) {
			t.Fatalf("missing structured location: %s", data)
		}
		if frames := bytes.Count(data, []byte("\n  ")); frames == 0 || frames > maxFrames {
			t.Fatalf("bad frame count: %d", frames)
		}
	}
	for _, label := range []string{secret, "token\nforged: value", strings.Repeat("a", 65), "C:\\private\\user"} {
		data := makeReport(label, secret)
		if !bytes.Contains(data, []byte("component: unknown\n")) || bytes.Contains(data, []byte(label)) {
			t.Fatal("unsafe component was retained")
		}
	}
}

//go:noinline
func deepReport(depth int) []byte {
	if depth == 0 {
		return makeReport("deep-stack", hostileValue{})
	}
	return deepReport(depth - 1)
}

func TestDeepStackBound(t *testing.T) {
	data := deepReport(100)
	if len(data) > maxReportBytes || bytes.Count(data, []byte("\n  ")) != maxFrames {
		t.Fatalf("deep report exceeded bound or lost frames: bytes=%d frames=%d", len(data), bytes.Count(data, []byte("\n  ")))
	}
}

func TestRetentionConcurrentAndForeignFiles(t *testing.T) {
	r := openRecorder(t)
	foreign := filepath.Join(r.dir, "crash-user-notes.txt")
	if err := os.WriteFile(foreign, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	oldest := filepath.Join(r.dir, "crash-20000101T000000.000000000Z-00000000000000000000000000000000.txt")
	if err := os.WriteFile(oldest, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	errors := make(chan error, 40)
	for i := 0; i < 40; i++ {
		group.Add(1)
		go func() { defer group.Done(); errors <- r.Record("concurrent-test", hostileValue{}) }()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if reportName.MatchString(entry.Name()) {
			count++
		}
	}
	if count != maxReports {
		t.Fatalf("report count = %d", count)
	}
	if _, err := os.Lstat(oldest); !os.IsNotExist(err) {
		t.Fatal("oldest report was not removed")
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "keep" {
		t.Fatal("foreign file modified")
	}
}

func TestContainAndInstall(t *testing.T) {
	r := openRecorder(t)
	previous := Install(r)
	t.Cleanup(func() { Install(previous) })
	if Contain("safe-task", func() {}) {
		t.Fatal("normal return reported as panic")
	}
	if !Contain("safe-task", func() { panic(hostileValue{}) }) {
		t.Fatal("panic was not contained")
	}
	if len(reports(t, r)) != 1 {
		t.Fatal("contained panic was not recorded")
	}
	Install(nil)
	if err := Record("disabled", nil); err != ErrIO {
		t.Fatalf("unexpected disabled error: %v", err)
	}
}

func TestFixedIOErrorsAndNonregularFiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", file, filepath.Join(file, "child")} {
		if _, err := Open(path); err != ErrIO {
			t.Fatalf("Open should return fixed error: %v", err)
		}
	}
	r := openRecorder(t)
	if err := os.Remove(r.dir); err != nil {
		t.Fatal(err)
	}
	if err := r.Record("gone", "secret"); err != ErrIO {
		t.Fatalf("Record should return fixed error: %v", err)
	}
	r = openRecorder(t)
	name := "crash-20000101T000000.000000000Z-00000000000000000000000000000000.txt"
	if err := os.Mkdir(filepath.Join(r.dir, name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := r.Record("directory-target", nil); err != ErrIO {
		t.Fatalf("nonregular target accepted: %v", err)
	}
}

func TestRejectSymlinkDirectoryAndReport(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlink creation unavailable on this host")
	}
	if _, err := Open(filepath.Join(link, "reports")); err != ErrIO {
		t.Fatal("symlink ancestor accepted")
	}
	r := openRecorder(t)
	outside := filepath.Join(base, "outside.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	name := "crash-20000101T000000.000000000Z-00000000000000000000000000000000.txt"
	if err := os.Symlink(outside, filepath.Join(r.dir, name)); err != nil {
		t.Fatal(err)
	}
	if err := r.Record("symlink-target", nil); err != ErrIO {
		t.Fatal("symlink report accepted")
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "keep" {
		t.Fatal("symlink target changed")
	}
}

func TestFatalSubprocess(t *testing.T) {
	if mode := os.Getenv("OTONEIGO_CRASH_TEST_MODE"); mode != "" {
		if mode != "no-recorder" {
			r, err := Open(os.Getenv("OTONEIGO_CRASH_TEST_DIR"))
			if err != nil {
				os.Exit(99)
			}
			Install(r)
			if mode == "io-failure" {
				if err := os.Remove(r.dir); err != nil {
					os.Exit(99)
				}
			}
		}
		if mode == "terminate" {
			Terminate("framework", hostileValue{})
		}
		defer Fatal("main")
		if mode == "nil-panic" {
			panic(nil)
		}
		panic("PRIVATE_PANIC_TOKEN_DO_NOT_PRINT")
	}
	for _, mode := range []string{"deferred", "terminate", "no-recorder", "io-failure", "nil-panic"} {
		t.Run(mode, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "reports")
			command := exec.Command(os.Args[0], "-test.run=^TestFatalSubprocess$")
			command.Env = append(os.Environ(), "OTONEIGO_CRASH_TEST_MODE="+mode, "OTONEIGO_CRASH_TEST_DIR="+dir)
			output, err := command.CombinedOutput()
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 2 {
				t.Fatalf("expected exit 2, got %v: %s", err, output)
			}
			if len(output) != 0 {
				t.Fatalf("fatal wrote raw output: %s", output)
			}
			files, err := filepath.Glob(filepath.Join(dir, "crash-*.txt"))
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if mode == "no-recorder" || mode == "io-failure" {
				want = 0
			}
			if len(files) != want {
				t.Fatalf("expected %d reports, got %d", want, len(files))
			}
			for _, file := range files {
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(data, []byte("PRIVATE_PANIC")) {
					t.Fatal("raw panic was persisted")
				}
			}
		})
	}
}
