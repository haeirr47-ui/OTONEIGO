// Package crashreport records bounded crash locations without formatting panic
// values or collecting other goroutines, arguments, or application state.
package crashreport

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxFrames      = 48
	maxReportBytes = 16 * 1024
	maxReports     = 5
)

// ErrIO deliberately contains no path or underlying filesystem error.
var ErrIO = errors.New("CRASH_REPORT_IO_FAILED")

var reportName = regexp.MustCompile(`^crash-[0-9]{8}T[0-9]{6}\.[0-9]{9}Z-[0-9a-f]{32}\.txt$`)
var installed atomic.Pointer[Recorder]

// Recorder owns a dedicated local report directory. It has no dependency on
// application storage, diagnostics, or cache locks.
type Recorder struct {
	dir string
	mu  sync.Mutex
}

// Open creates or validates the report directory, rejecting links and reparse
// points throughout its path. Permissions are best effort on Windows.
func Open(dir string) (*Recorder, error) {
	abs, err := filepath.Abs(dir)
	if err != nil || dir == "" {
		return nil, ErrIO
	}
	if err := checkPath(abs, true); err != nil {
		return nil, ErrIO
	}
	if err := os.MkdirAll(abs, 0700); err != nil {
		return nil, ErrIO
	}
	if err := checkPath(abs, false); err != nil {
		return nil, ErrIO
	}
	_ = os.Chmod(abs, 0700)
	r := &Recorder{dir: abs}
	if r.retain(maxReports) != nil {
		return nil, ErrIO
	}
	return r, nil
}

// Install replaces the process recorder and returns the previous recorder.
// Passing nil disables recording. Fatal still exits when recording is disabled.
func Install(recorder *Recorder) *Recorder { return installed.Swap(recorder) }

// Record writes through the installed recorder, or returns ErrIO if none exists.
func Record(component string, value any) error {
	r := installed.Load()
	if r == nil {
		return ErrIO
	}
	return r.Record(component, value)
}

// Fatal must be deferred directly at an explicitly owned goroutine boundary.
// An unsafe panic terminates with a fixed exit code even if recording fails.
func Fatal(component string) {
	if value := recover(); value != nil {
		Terminate(component, value)
	}
}

// Terminate records an already recovered fatal panic and always exits with code 2.
// Framework panic handlers can use it without re-panicking or formatting value.
func Terminate(component string, value any) {
	_ = Record(component, value)
	os.Exit(2)
}

// Contain is only for audited tasks whose abandoned state is safe to discard.
// It reports whether fn panicked; it must not wrap arbitrary long-lived workers.
func Contain(component string, fn func()) (panicked bool) {
	defer func() {
		if value := recover(); value != nil {
			panicked = true
			_ = Record(component, value)
		}
	}()
	fn()
	return false
}

// Record never invokes String, Error, or marshal methods on value. Only its
// fixed language-level kind is retained; even user-defined type names are omitted.
func (r *Recorder) Record(component string, value any) error {
	if r == nil {
		return ErrIO
	}
	data := makeReport(component, value)
	r.mu.Lock()
	defer r.mu.Unlock()
	if checkPath(r.dir, false) != nil {
		return ErrIO
	}
	// Reserve a slot before creating anything. A locked old report or other
	// retention failure must never permit an unbounded sequence of new files.
	if r.retain(maxReports-1) != nil {
		return ErrIO
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return ErrIO
	}
	name := "crash-" + time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(random[:]) + ".txt"
	path := filepath.Join(r.dir, name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrIO
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		// Even if cleanup fails, the reserved slot keeps the total bounded;
		// subsequent attempts must pass retention before creating another file.
		if checkPath(r.dir, false) == nil {
			_ = os.Remove(path)
		}
		return ErrIO
	}
	return nil
}

func makeReport(component string, value any) []byte {
	category := "nil"
	if typ := reflect.TypeOf(value); typ != nil {
		category = typ.Kind().String()
	}
	var out strings.Builder
	out.WriteString("OTONEIGO crash report v1\ncomponent: " + safeLabel(component, 64) + "\npanic_category: " + category + "\npanic_value: [redacted]\nstack:\n")
	var pcs [maxFrames]uintptr
	n := runtime.Callers(3, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	for count := 0; count < maxFrames; count++ {
		frame, more := frames.Next()
		function := frame.Function
		if slash := strings.LastIndexByte(function, '/'); slash >= 0 {
			function = function[slash+1:]
		}
		line := "  " + safeLabel(function, 160) + " " + safeLabel(filepath.Base(frame.File), 100) + ":" + strconv.Itoa(frame.Line) + "\n"
		if out.Len()+len(line) > maxReportBytes {
			break
		}
		out.WriteString(line)
		if !more {
			break
		}
	}
	return []byte(out.String())
}

// Accept only short static labels; invalid input is discarded in full so URLs,
// paths, control characters, and arbitrary panic-adjacent data cannot be echoed.
func safeLabel(value string, limit int) string {
	if value == "" || len(value) > limit {
		return "unknown"
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-()[]*", c)) {
			return "unknown"
		}
	}
	return value
}

func (r *Recorder) retain(limit int) error {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return ErrIO
	}
	names := make([]string, 0)
	for _, entry := range entries {
		if !reportName.MatchString(entry.Name()) {
			continue
		}
		path := filepath.Join(r.dir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || isReparse(path) {
			return ErrIO
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for len(names) > limit {
		if checkPath(r.dir, false) != nil {
			return ErrIO
		}
		path := filepath.Join(r.dir, names[0])
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || isReparse(path) {
			return ErrIO
		}
		if os.Remove(path) != nil {
			return ErrIO
		}
		names = names[1:]
	}
	return nil
}

func checkPath(path string, allowMissing bool) error {
	for {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) && allowMissing {
			// Validate all existing ancestors before creating anything.
		} else if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || isReparse(path) {
			return ErrIO
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}
