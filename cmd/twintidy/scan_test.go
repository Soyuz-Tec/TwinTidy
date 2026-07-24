package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeScanFixture creates the fixture tree in the working directory rather
// than the OS temp directory: the scanner's safety policy intentionally
// refuses AppData/Temp paths as non-user-created, mirroring production.
func writeScanFixture(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp(".", "scan-cli-test-")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	if root, err = filepath.Abs(root); err != nil {
		t.Fatalf("Abs failed: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	duplicate := strings.Repeat("twintidy scan fixture content\n", 64)
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		filepath.Join(root, "a.txt"):           duplicate,
		filepath.Join(root, "sub", "b.txt"):    duplicate,
		filepath.Join(root, "unique.txt"):      "unique content\n",
		filepath.Join(root, "sub", "tiny.txt"): "x",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestRunScanFindsDuplicatesAndExitsOne(t *testing.T) {
	root := writeScanFixture(t)
	var stdout, stderr bytes.Buffer

	code := runScan([]string{"--quiet", root}, &stdout, &stderr)
	if code != scanExitDuplicates {
		t.Fatalf("expected exit %d, got %d (stderr: %s)", scanExitDuplicates, code, stderr.String())
	}

	var document map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}
	report := stdout.String()
	if !strings.Contains(report, "a.txt") || !strings.Contains(report, "b.txt") {
		t.Fatalf("expected both duplicates in report, got: %s", report)
	}
	if strings.Contains(report, "unique.txt") {
		t.Fatalf("unique file must not appear in report: %s", report)
	}
}

func TestRunScanMinSizeFiltersAllAndExitsZero(t *testing.T) {
	root := writeScanFixture(t)
	var stdout, stderr bytes.Buffer

	code := runScan([]string{"--quiet", "--min-size", "1048576", root}, &stdout, &stderr)
	if code != scanExitNoDuplicates {
		t.Fatalf("expected exit %d, got %d (stderr: %s)", scanExitNoDuplicates, code, stderr.String())
	}
}

func TestRunScanExcludePathRemovesDuplicatePartner(t *testing.T) {
	root := writeScanFixture(t)
	var stdout, stderr bytes.Buffer

	code := runScan([]string{"--quiet", "--exclude", filepath.Join(root, "sub"), root}, &stdout, &stderr)
	if code != scanExitNoDuplicates {
		t.Fatalf("expected exit %d after excluding one partner, got %d (stderr: %s)", scanExitNoDuplicates, code, stderr.String())
	}
}

func TestRunScanWritesReportFileAtomically(t *testing.T) {
	root := writeScanFixture(t)
	out := filepath.Join(writeScanFixture(t), "report.csv")
	var stdout, stderr bytes.Buffer

	code := runScan([]string{"--quiet", "--format", "csv", "--out", out, root}, &stdout, &stderr)
	if code != scanExitDuplicates {
		t.Fatalf("expected exit %d, got %d (stderr: %s)", scanExitDuplicates, code, stderr.String())
	}
	content, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("report file missing: %v", err)
	}
	if !strings.Contains(string(content), "a.txt") {
		t.Fatalf("expected duplicate in CSV report, got: %s", content)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout when writing to a file, got: %s", stdout.String())
	}
}

func TestRunScanRejectsBadInvocation(t *testing.T) {
	cases := [][]string{
		{},
		{"--format", "xml", "somewhere"},
		{"--not-a-flag"},
	}
	for _, args := range cases {
		var stdout, stderr bytes.Buffer
		if code := runScan(args, &stdout, &stderr); code != scanExitError {
			t.Fatalf("runScan(%v) = %d, want %d", args, code, scanExitError)
		}
	}
}
