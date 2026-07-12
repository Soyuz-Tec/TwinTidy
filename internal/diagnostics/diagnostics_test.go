package diagnostics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCrashReportIsWrittenToLocalAppData(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("LOCALAPPDATA", tempDir)

	resetForTest(t)
	if err := Init("TwinTidyTest"); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer func() {
		Close()
		resetForTest(t)
	}()

	Logf("diagnostics test message")
	path := WriteCrashReport("unit test", "boom", []byte("stack line\n"), map[string]string{"operation": "test"})
	if path == "" {
		t.Fatal("expected crash report path")
	}

	expectedRoot := filepath.Join(tempDir, "TwinTidyTest", "logs")
	if !strings.HasPrefix(path, expectedRoot) {
		t.Fatalf("crash report path %q does not start with %q", path, expectedRoot)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read crash report: %v", err)
	}
	report := string(data)
	for _, want := range []string{
		"TwinTidy Crash Report",
		"Scope: unit test",
		"Panic: boom",
		"- operation: test",
		"stack line",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("crash report missing %q:\n%s", want, report)
		}
	}

	if SessionLogPath() == "" {
		t.Fatal("expected session log path")
	}
}

func TestCrashReportRedactsUnapprovedFieldsAndLocalPaths(t *testing.T) {
	report := buildCrashReport(
		"unit test",
		"boom",
		[]byte("trimmed stack\n"),
		map[string]string{
			"operation": "scan",
			"folder":    `C:\Users\person\Private`,
		},
	)

	if !strings.Contains(report, "- operation: scan") {
		t.Fatalf("safe operation field was not retained:\n%s", report)
	}
	if !strings.Contains(report, "- folder: [redacted]") {
		t.Fatalf("folder field was not redacted:\n%s", report)
	}
	if strings.Contains(report, `C:\Users\person\Private`) {
		t.Fatalf("crash report exposed a local folder:\n%s", report)
	}
	if !strings.Contains(report, "WorkingDir: [redacted local path]") {
		t.Fatalf("working directory was not redacted:\n%s", report)
	}
}

func TestArgumentSummaryKeepsOptionsAndRedactsValues(t *testing.T) {
	got := argumentSummary([]string{"--ui-smoke-test", `C:\Private\file.txt`, "-h"})
	want := "--ui-smoke-test [redacted] -h"
	if got != want {
		t.Fatalf("argumentSummary() = %q, want %q", got, want)
	}
	if got := argumentSummary(nil); got != "none" {
		t.Fatalf("argumentSummary(nil) = %q, want none", got)
	}
}

func TestPanicToErrorRecoversFromDeferredCaller(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	resetForTest(t)
	t.Cleanup(func() { resetForTest(t) })

	err := panicAsErrorForTest()
	if err == nil {
		t.Fatal("panic was not converted to an error")
	}
	if !strings.Contains(err.Error(), "panic conversion test") {
		t.Fatalf("panic error lacks scope: %v", err)
	}

	crashFiles, globErr := filepath.Glob(filepath.Join(LogDir(), "crash-*.txt"))
	if globErr != nil {
		t.Fatalf("Glob crash reports: %v", globErr)
	}
	if len(crashFiles) != 1 {
		t.Fatalf("crash report count = %d, want 1", len(crashFiles))
	}
}

func panicAsErrorForTest() (err error) {
	defer func() {
		recovered := recover()
		err = PanicToError("panic conversion test", recovered, nil)
	}()
	panic("expected test panic")
}

func resetForTest(t *testing.T) {
	t.Helper()

	mu.Lock()
	defer mu.Unlock()

	if logFile != nil {
		_ = logFile.Close()
	}
	logFile = nil
	logDirPath = ""
	sessionLogPath = ""
}
