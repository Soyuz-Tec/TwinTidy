package scanner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScanFindsExactMatchesWithDifferentNames(t *testing.T) {
	root := userFileTestRoot(t)
	content := []byte("invoice duplicate payload")
	writeTestFile(t, filepath.Join(root, "January invoice.pdf"), content)
	writeTestFile(t, filepath.Join(root, "renamed-copy.bin"), content)
	writeTestFile(t, filepath.Join(root, "same-size-not-duplicate.txt"), []byte("different file payload!"))

	engine := NewEngine(2)
	groups, err := engine.Scan(context.Background(), []string{root}, make(chan Progress, 128))
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 duplicate group, got %d: %#v", len(groups), groups)
	}
	if len(groups[0].Files) != 2 {
		t.Fatalf("expected 2 duplicate files, got %d", len(groups[0].Files))
	}

	paths := map[string]bool{}
	for _, file := range groups[0].Files {
		paths[filepath.Base(file.Path)] = true
	}
	if !paths["January invoice.pdf"] || !paths["renamed-copy.bin"] {
		t.Fatalf("duplicate group did not include expected differently named files: %#v", paths)
	}
}

func TestScanRejectsSameSizeDifferentContent(t *testing.T) {
	root := userFileTestRoot(t)
	writeTestFile(t, filepath.Join(root, "a.txt"), []byte("same-size-a"))
	writeTestFile(t, filepath.Join(root, "b.txt"), []byte("same-size-b"))

	engine := NewEngine(2)
	groups, err := engine.Scan(context.Background(), []string{root}, make(chan Progress, 128))
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("expected no duplicates, got %#v", groups)
	}
}

func TestScanRejectsSameBoundaryDifferentMiddle(t *testing.T) {
	root := userFileTestRoot(t)
	head := repeatByte('h', boundaryReadSize)
	tail := repeatByte('t', boundaryReadSize)
	left := append(append([]byte{}, head...), repeatByte('a', boundaryReadSize)...)
	left = append(left, tail...)
	right := append(append([]byte{}, head...), repeatByte('b', boundaryReadSize)...)
	right = append(right, tail...)

	writeTestFile(t, filepath.Join(root, "left.bin"), left)
	writeTestFile(t, filepath.Join(root, "right.bin"), right)

	engine := NewEngine(2)
	groups, err := engine.Scan(context.Background(), []string{root}, make(chan Progress, 128))
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("expected full hashing to reject boundary-only matches, got %#v", groups)
	}
}

func TestScanSkipsSystemAndApplicationFiles(t *testing.T) {
	root := userFileTestRoot(t)
	content := []byte("same duplicate bytes")
	writeTestFile(t, filepath.Join(root, "keep-a.pdf"), content)
	writeTestFile(t, filepath.Join(root, "keep-b.pdf"), content)
	writeTestFile(t, filepath.Join(root, "setup-a.exe"), content)
	writeTestFile(t, filepath.Join(root, "setup-b.exe"), content)
	writeTestFile(t, filepath.Join(root, "node_modules", "package-a.pdf"), []byte("dependency duplicate"))
	writeTestFile(t, filepath.Join(root, "node_modules", "package-b.pdf"), []byte("dependency duplicate"))

	engine := NewEngine(2)
	groups, err := engine.Scan(context.Background(), []string{root}, make(chan Progress, 128))
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected only user PDF duplicate group, got %d: %#v", len(groups), groups)
	}
	for _, file := range groups[0].Files {
		name := filepath.Base(file.Path)
		if name != "keep-a.pdf" && name != "keep-b.pdf" {
			t.Fatalf("scanner included non-user file %q", file.Path)
		}
	}
}

func TestSurfaceScanCategorizesUserFiles(t *testing.T) {
	root := userFileTestRoot(t)
	writeTestFile(t, filepath.Join(root, "a.pdf"), []byte("pdf"))
	writeTestFile(t, filepath.Join(root, "b.docx"), []byte("word"))
	writeTestFile(t, filepath.Join(root, "c.xlsx"), []byte("excel"))
	writeTestFile(t, filepath.Join(root, "d.txt"), []byte("text"))
	writeTestFile(t, filepath.Join(root, "ignored.exe"), []byte("app"))

	engine := NewEngine(2)
	report, err := engine.SurfaceScan(context.Background(), []string{root}, make(chan Progress, 128))
	if err != nil {
		t.Fatalf("SurfaceScan returned error: %v", err)
	}
	if report.TotalFiles != 4 {
		t.Fatalf("expected 4 user files, got %d", report.TotalFiles)
	}
	if report.CategoryStats[CategoryPDF].Files != 1 ||
		report.CategoryStats[CategoryWord].Files != 1 ||
		report.CategoryStats[CategoryExcel].Files != 1 ||
		report.CategoryStats[CategoryText].Files != 1 {
		t.Fatalf("unexpected category stats: %#v", report.CategoryStats)
	}
	if report.SkippedSystemItems == 0 {
		t.Fatalf("expected executable to be skipped")
	}
}

func TestScanFilesHonorsCategoryFilter(t *testing.T) {
	root := userFileTestRoot(t)
	pdfPayload := []byte("pdf duplicate payload")
	wordPayload := []byte("word duplicate payload")
	writeTestFile(t, filepath.Join(root, "a.pdf"), pdfPayload)
	writeTestFile(t, filepath.Join(root, "b.pdf"), pdfPayload)
	writeTestFile(t, filepath.Join(root, "a.docx"), wordPayload)
	writeTestFile(t, filepath.Join(root, "b.docx"), wordPayload)

	engine := NewEngine(2)
	report, err := engine.SurfaceScan(context.Background(), []string{root}, make(chan Progress, 128))
	if err != nil {
		t.Fatalf("SurfaceScan returned error: %v", err)
	}

	groups, err := engine.ScanFiles(context.Background(), report.Files, ScanOptions{
		Categories:    map[FileCategory]bool{CategoryPDF: true},
		UserFilesOnly: true,
	}, make(chan Progress, 128))
	if err != nil {
		t.Fatalf("ScanFiles returned error: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected one PDF group, got %d: %#v", len(groups), groups)
	}
	for _, file := range groups[0].Files {
		if filepath.Ext(file.Path) != ".pdf" {
			t.Fatalf("expected only PDF records, got %s", file.Path)
		}
	}
}

func TestScanFilesRefreshesMetadataBeforeGrouping(t *testing.T) {
	root := userFileTestRoot(t)
	want := []byte("current duplicate payload")
	left := filepath.Join(root, "left.txt")
	right := filepath.Join(root, "right.txt")
	writeTestFile(t, left, want)
	writeTestFile(t, right, []byte("old"))

	engine := NewEngine(2)
	report, err := engine.SurfaceScan(context.Background(), []string{root}, make(chan Progress, 128))
	if err != nil {
		t.Fatalf("SurfaceScan returned error: %v", err)
	}
	writeTestFile(t, right, want)

	groups, err := engine.ScanFiles(context.Background(), report.Files, DefaultScanOptions(), make(chan Progress, 128))
	if err != nil {
		t.Fatalf("ScanFiles returned error: %v", err)
	}
	if len(groups) != 1 || len(groups[0].Files) != 2 {
		t.Fatalf("expected refreshed metadata to reveal one duplicate group, got %#v", groups)
	}
	for _, file := range groups[0].Files {
		if file.Size != int64(len(want)) {
			t.Fatalf("expected refreshed size %d for %s, got %d", len(want), file.Path, file.Size)
		}
		if file.Identity == (FileIdentity{}) {
			t.Fatalf("expected stable identity for %s", file.Path)
		}
	}
}

func TestSurfaceScanBindsEveryRecordToAuthorizedRoot(t *testing.T) {
	root := userFileTestRoot(t)
	writeTestFile(t, filepath.Join(root, "bound.txt"), []byte("scope-bound record"))

	report, err := NewEngine(1).SurfaceScan(context.Background(), []string{root}, nil)
	if err != nil {
		t.Fatalf("SurfaceScan returned error: %v", err)
	}
	if len(report.Files) != 1 {
		t.Fatalf("expected one record, got %#v", report.Files)
	}
	record := report.Files[0]
	if record.Scope.RootFinalPath == "" || record.Scope.RootIdentity == (FileIdentity{}) || record.Scope.RootIsFile {
		t.Fatalf("record has invalid directory scope: %#v", record.Scope)
	}
	if !pathWithinScope(record.Path, record.Scope) {
		t.Fatalf("record %q is outside scope %#v", record.Path, record.Scope)
	}
	if err := ValidateRecordScope(record); err != nil {
		t.Fatalf("ValidateRecordScope rejected unchanged record: %v", err)
	}
	file, err := OpenVerifiedRecordForRead(record)
	if err != nil {
		t.Fatalf("OpenVerifiedRecordForRead rejected unchanged record: %v", err)
	}
	content, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read verified record: read=%v close=%v", readErr, closeErr)
	}
	if string(content) != "scope-bound record" {
		t.Fatalf("verified read returned %q", content)
	}

	// AuthorizedScope must remain comparable so request validation can bind the
	// exact same value without pointer identity or mutable state.
	seen := map[AuthorizedScope]bool{record.Scope: true}
	if !seen[record.Scope] {
		t.Fatal("authorized scope is not stable as a comparable map key")
	}
}

func TestScanFilesRejectsUnscopedInventory(t *testing.T) {
	_, err := NewEngine(1).ScanFiles(context.Background(), []FileRecord{{
		Path:     filepath.Join(userFileTestRoot(t), "unscoped.txt"),
		Category: CategoryText,
	}}, DefaultScanOptions(), nil)
	if !errors.Is(err, errMissingScope) {
		t.Fatalf("ScanFiles error = %v, want errMissingScope", err)
	}
}

func TestPathWithinScopeUsesComponentBoundary(t *testing.T) {
	parent := userFileTestRoot(t)
	root := filepath.Join(parent, "selected")
	scope := AuthorizedScope{RootFinalPath: root, RootIdentity: FileIdentity{VolumeSerial: 1}}
	if !pathWithinScope(filepath.Join(root, "child", "file.txt"), scope) {
		t.Fatal("child path was rejected")
	}
	if pathWithinScope(filepath.Join(parent, "selected-other", "file.txt"), scope) {
		t.Fatal("sibling path sharing the root string prefix was accepted")
	}
	if pathWithinScope(parent, scope) {
		t.Fatal("parent path was accepted")
	}

	scope.RootIsFile = true
	if !pathWithinScope(root, scope) || pathWithinScope(filepath.Join(root, "child"), scope) {
		t.Fatal("single-file scope did not require the exact final path")
	}
}

func TestSurfaceScanStopsAtActionableFileLimit(t *testing.T) {
	root := userFileTestRoot(t)
	writeTestFile(t, filepath.Join(root, "a.txt"), []byte("a"))
	writeTestFile(t, filepath.Join(root, "b.txt"), []byte("b"))
	writeTestFile(t, filepath.Join(root, "c.txt"), []byte("c"))

	engine := NewEngineWithLimits(2, ScanLimits{MaxRoots: 1, MaxDirectories: 10, MaxFiles: 2})
	_, err := engine.SurfaceScan(context.Background(), []string{root}, nil)
	if !errors.Is(err, ErrScanLimitExceeded) {
		t.Fatalf("SurfaceScan error = %v, want ErrScanLimitExceeded", err)
	}
	if !strings.Contains(err.Error(), "maximum of 2 file") || !strings.Contains(err.Error(), "select fewer or smaller folders") {
		t.Fatalf("scan limit error is not actionable: %v", err)
	}
}

func TestSurfaceScanStopsAtActionableDirectoryLimit(t *testing.T) {
	root := userFileTestRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "child"), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	engine := NewEngineWithLimits(1, ScanLimits{MaxRoots: 1, MaxDirectories: 1, MaxFiles: 10})
	_, err := engine.SurfaceScan(context.Background(), []string{root}, nil)
	if !errors.Is(err, ErrScanLimitExceeded) {
		t.Fatalf("SurfaceScan error = %v, want ErrScanLimitExceeded", err)
	}
	if !strings.Contains(err.Error(), "maximum of 1 directory") {
		t.Fatalf("directory limit error is not actionable: %v", err)
	}
}

func TestDirectoryEnumerationChecksCancellationWithinBatch(t *testing.T) {
	root := userFileTestRoot(t)
	for index := 0; index < directoryReadBatchSize+10; index++ {
		writeTestFile(t, filepath.Join(root, fmt.Sprintf("file-%04d.txt", index)), []byte("x"))
	}
	scope, info, err := authorizeScanRoot(root)
	if err != nil {
		t.Fatalf("authorizeScanRoot failed: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("test root is not a directory")
	}

	ctx, cancel := context.WithCancel(context.Background())
	work := directoryWork{path: scope.RootFinalPath, scope: scope}
	queue := newDirectoryQueue([]directoryWork{work})
	budget := scanBudget{limits: ScanLimits{MaxRoots: 1, MaxDirectories: 10, MaxFiles: 1_000}}
	var directories, ignored, skipped int64
	visited := 0
	err = NewEngine(1).readDirectory(
		ctx,
		work,
		queue,
		&budget,
		func(string, AuthorizedScope) error {
			visited++
			cancel()
			return nil
		},
		&directories,
		&ignored,
		&skipped,
		nil,
		time.Now(),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("readDirectory error = %v, want context.Canceled", err)
	}
	if visited != 1 {
		t.Fatalf("enumeration visited %d files after cancellation, want 1", visited)
	}
}

func TestCopyWithContextStopsBetweenReads(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := &cancelAfterFirstReader{cancel: cancel}
	var destination bytes.Buffer

	written, err := copyWithContext(ctx, &destination, reader, make([]byte, 32))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("copyWithContext error = %v, want context.Canceled", err)
	}
	if written == 0 {
		t.Fatal("expected the first read to complete before cancellation")
	}
	if reader.reads != 1 {
		t.Fatalf("reader performed %d reads after cancellation, want 1", reader.reads)
	}
}

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}

func userFileTestRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp(".", "scanner-test-")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("Abs failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(absRoot)
	})
	return absRoot
}

func repeatByte(b byte, count int) []byte {
	data := make([]byte, count)
	for i := range data {
		data[i] = b
	}
	return data
}

type cancelAfterFirstReader struct {
	cancel context.CancelFunc
	reads  int
}

func (r *cancelAfterFirstReader) Read(buffer []byte) (int, error) {
	if r.reads > 0 {
		return 0, io.EOF
	}
	r.reads++
	count := copy(buffer, []byte("first chunk"))
	r.cancel()
	return count, nil
}
