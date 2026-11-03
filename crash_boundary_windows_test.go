//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/voici5986/otoneigo/internal/crashreport"
	"github.com/voici5986/otoneigo/internal/lifecycle"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestHostPanicProcess(t *testing.T) {
	mode := os.Getenv("OTONEIGO_HOST_PANIC_TEST")
	if mode == "" {
		return
	}
	r, err := crashreport.Open(os.Getenv("OTONEIGO_HOST_PANIC_DIR"))
	if err != nil {
		t.Fatal(err)
	}
	crashreport.Install(r)
	switch mode {
	case "wails":
		handleApplicationPanic(&application.PanicDetails{Error: errors.New("Cookie=private https://secret.invalid"), FullStackTrace: "Token=private", StackTrace: "C:/Users/private"})
	case "close-gate":
		g := lifecycle.NewCloseGate(time.Millisecond, func() { panic("Token=private https://secret.invalid") })
		g.Request()
		select {}
	}
	t.Fatal("fatal boundary returned")
}

func TestHostFatalBoundariesRecordAndExit(t *testing.T) {
	for _, component := range []string{"wails", "close-gate"} {
		t.Run(component, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHostPanicProcess$")
			cmd.Env = append(os.Environ(), "OTONEIGO_HOST_PANIC_TEST="+component, "OTONEIGO_HOST_PANIC_DIR="+dir)
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if ctx.Err() != nil || !errors.As(err, &exit) || exit.ExitCode() != 2 {
				t.Fatalf("fatal result: %v %s", err, output)
			}
			files, _ := filepath.Glob(filepath.Join(dir, "crash-*.txt"))
			if len(files) != 1 {
				t.Fatal("missing report")
			}
			data, err := os.ReadFile(files[0])
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "component: "+component) {
				t.Fatal("missing component")
			}
			for _, secret := range []string{"private", "secret.invalid", "Cookie=", "Token=", "C:/Users"} {
				if strings.Contains(string(data)+string(output), secret) {
					t.Fatal("raw panic data leaked")
				}
			}
		})
	}
}
