//go:build windows

package scanner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsFileIdentityIsStableAndDetectsReplacement(t *testing.T) {
	root := userFileTestRoot(t)
	path := filepath.Join(root, "identity.txt")
	backup := filepath.Join(root, "identity-original.txt")
	writeTestFile(t, path, []byte("same bytes"))

	first := mustFileSnapshot(t, path)
	second := mustFileSnapshot(t, path)
	if first.identity != second.identity {
		t.Fatalf("identity changed without replacement: %#v != %#v", first.identity, second.identity)
	}

	if err := os.Rename(path, backup); err != nil {
		t.Fatalf("Rename failed: %v", err)
	}
	writeTestFile(t, path, []byte("same bytes"))
	replacement := mustFileSnapshot(t, path)
	if first.identity == replacement.identity {
		t.Fatalf("replacement reused the original live file identity: %#v", replacement.identity)
	}
}

func TestWindowsHardLinksAreProtected(t *testing.T) {
	root := userFileTestRoot(t)
	original := filepath.Join(root, "original.txt")
	link := filepath.Join(root, "linked.txt")
	writeTestFile(t, original, []byte("hard link payload"))
	if err := os.Link(original, link); err != nil {
		t.Skipf("hard links unavailable on test volume: %v", err)
	}

	snapshot := mustFileSnapshot(t, original)
	if snapshot.linkCount < 2 {
		t.Fatalf("link count = %d, want at least 2", snapshot.linkCount)
	}
	_, err := refreshFileRecord(FileRecord{Path: original, Category: CategoryText})
	if !errors.Is(err, errHardLinkedFile) {
		t.Fatalf("refreshFileRecord error = %v, want errHardLinkedFile", err)
	}
}

func TestScanDoesNotReportHardLinksAsReclaimableDuplicates(t *testing.T) {
	root := userFileTestRoot(t)
	original := filepath.Join(root, "physical-file.txt")
	link := filepath.Join(root, "second-name.txt")
	writeTestFile(t, original, []byte("one physical allocation"))
	if err := os.Link(original, link); err != nil {
		t.Skipf("hard links unavailable on test volume: %v", err)
	}

	groups, err := NewEngine(1).Scan(context.Background(), []string{root}, nil)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("hard-link aliases were reported as reclaimable duplicates: %#v", groups)
	}
}

func TestWindowsNamedStreamsAreProtected(t *testing.T) {
	root := userFileTestRoot(t)
	path := filepath.Join(root, "streamed.txt")
	streamPath := path + ":TwinTidySafetyTest"
	writeTestFile(t, path, []byte("default stream"))
	if err := os.WriteFile(streamPath, []byte("named stream"), 0o644); err != nil {
		t.Skipf("alternate data streams unavailable on test volume: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(streamPath) })

	snapshot := mustFileSnapshot(t, path)
	if snapshot.namedStreams == 0 {
		t.Fatal("expected the named stream to be detected")
	}
	_, err := refreshFileRecord(FileRecord{Path: path, Category: CategoryText})
	if !errors.Is(err, errNamedStreamFile) {
		t.Fatalf("refreshFileRecord error = %v, want errNamedStreamFile", err)
	}
}

func TestWindowsSnapshotReadsStreamsFromExpectedOpenHandle(t *testing.T) {
	root := userFileTestRoot(t)
	path := filepath.Join(root, "handle-streams.txt")
	movedOriginal := filepath.Join(root, "handle-streams-original.txt")
	writeTestFile(t, path, []byte("original object"))
	handle, err := openVerificationFile(path, true)
	if err != nil {
		t.Fatalf("openVerificationFile failed: %v", err)
	}
	defer handle.Close()
	initial, err := snapshotOpenFile(handle, path)
	if err != nil {
		t.Fatalf("snapshotOpenFile initial failed: %v", err)
	}
	if err := os.Rename(path, movedOriginal); err != nil {
		t.Fatalf("Rename original failed: %v", err)
	}
	writeTestFile(t, path, []byte("replacement object"))
	streamPath := path + ":TwinTidyReplacementStream"
	if err := os.WriteFile(streamPath, []byte("replacement stream"), 0o644); err != nil {
		t.Skipf("alternate data streams unavailable on test volume: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(streamPath) })

	current, err := snapshotOpenFile(handle, path)
	if err != nil {
		t.Fatalf("snapshotOpenFile failed: %v", err)
	}
	if current.identity != initial.identity {
		t.Fatalf("open-handle identity changed: %#v != %#v", current.identity, initial.identity)
	}
	if current.namedStreams != 0 {
		t.Fatalf("snapshot mixed replacement-path streams into original handle: %d", current.namedStreams)
	}
}

func TestScanDoesNotGroupAFileWithNamedStreams(t *testing.T) {
	root := userFileTestRoot(t)
	left := filepath.Join(root, "left.txt")
	right := filepath.Join(root, "right.txt")
	streamPath := right + ":TwinTidySafetyTest"
	writeTestFile(t, left, []byte("same default stream"))
	writeTestFile(t, right, []byte("same default stream"))
	if err := os.WriteFile(streamPath, []byte("unique named stream"), 0o644); err != nil {
		t.Skipf("alternate data streams unavailable on test volume: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(streamPath) })

	groups, err := NewEngine(1).Scan(context.Background(), []string{root}, nil)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("file with named streams was grouped as an exact duplicate: %#v", groups)
	}
}

func TestSurfaceScanRejectsSelectedJunctionRoot(t *testing.T) {
	container := userFileTestRoot(t)
	target := filepath.Join(container, "target")
	alias := filepath.Join(container, "selected-alias")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	createTestJunction(t, alias, target)

	_, err := NewEngine(1).SurfaceScan(context.Background(), []string{alias}, nil)
	if !errors.Is(err, errReparsePoint) {
		t.Fatalf("SurfaceScan error = %v, want errReparsePoint", err)
	}
}

func TestSurfaceScanRejectsJunctionInSelectedRootAncestor(t *testing.T) {
	container := userFileTestRoot(t)
	target := filepath.Join(container, "target")
	selected := filepath.Join(target, "nested", "selected")
	alias := filepath.Join(container, "ancestor-alias")
	if err := os.MkdirAll(selected, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	createTestJunction(t, alias, target)

	selectedThroughAlias := filepath.Join(alias, "nested", "selected")
	_, err := NewEngine(1).SurfaceScan(context.Background(), []string{selectedThroughAlias}, nil)
	if !errors.Is(err, errReparsePoint) {
		t.Fatalf("SurfaceScan error = %v, want errReparsePoint for ancestor junction", err)
	}
}

func TestSurfaceScanSkipsNestedJunction(t *testing.T) {
	container := userFileTestRoot(t)
	root := filepath.Join(container, "scan-root")
	target := filepath.Join(container, "outside-target")
	alias := filepath.Join(root, "nested-alias")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll root failed: %v", err)
	}
	writeTestFile(t, filepath.Join(target, "outside-a.txt"), []byte("outside duplicate"))
	writeTestFile(t, filepath.Join(target, "outside-b.txt"), []byte("outside duplicate"))
	createTestJunction(t, alias, target)

	report, err := NewEngine(1).SurfaceScan(context.Background(), []string{root}, nil)
	if err != nil {
		t.Fatalf("SurfaceScan returned error: %v", err)
	}
	if len(report.Files) != 0 {
		t.Fatalf("nested junction target was traversed: %#v", report.Files)
	}
	if report.SkippedSystemItems == 0 {
		t.Fatal("expected nested junction to increment protected-item count")
	}
}

func TestResolveScanRootAllowsNonTraversalProviderReparsePoint(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	pathPtr, err := windows.UTF16PtrFromString(workingDirectory)
	if err != nil {
		t.Fatalf("UTF16PtrFromString failed: %v", err)
	}
	attributes, err := windows.GetFileAttributes(pathPtr)
	if err != nil {
		t.Fatalf("GetFileAttributes failed: %v", err)
	}
	if attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		t.Skipf("working directory %q is not backed by a provider reparse point (attributes 0x%x)", workingDirectory, attributes)
	}
	unsafeTraversal, err := pathIsTraversalReparsePoint(workingDirectory)
	if err != nil {
		t.Fatalf("pathIsTraversalReparsePoint failed: %v", err)
	}
	if unsafeTraversal {
		t.Skip("working directory is a traversal reparse point")
	}
	if _, err := resolveScanRoot(workingDirectory); err != nil {
		t.Fatalf("safe provider reparse root was rejected: %v", err)
	}
}

func TestValidateRecordScopeRejectsSelectedRootReplacement(t *testing.T) {
	container := userFileTestRoot(t)
	root := filepath.Join(container, "scan-root")
	originalMoved := filepath.Join(container, "original-root")
	path := filepath.Join(root, "record.txt")
	writeTestFile(t, path, []byte("same bytes at replacement path"))

	report, err := NewEngine(1).SurfaceScan(context.Background(), []string{root}, nil)
	if err != nil {
		t.Fatalf("SurfaceScan returned error: %v", err)
	}
	if len(report.Files) != 1 {
		t.Fatalf("expected one record, got %#v", report.Files)
	}
	if err := os.Rename(root, originalMoved); err != nil {
		t.Fatalf("Rename root failed: %v", err)
	}
	writeTestFile(t, path, []byte("same bytes at replacement path"))

	err = ValidateRecordScope(report.Files[0])
	if !errors.Is(err, errRootChanged) {
		t.Fatalf("ValidateRecordScope error = %v, want errRootChanged", err)
	}
	_, err = NewEngine(1).ScanFiles(context.Background(), report.Files, DefaultScanOptions(), nil)
	if !errors.Is(err, errRootChanged) {
		t.Fatalf("ScanFiles error = %v, want errRootChanged after root replacement", err)
	}
}

func TestValidateRecordScopeRejectsNestedJunctionSwap(t *testing.T) {
	container := userFileTestRoot(t)
	root := filepath.Join(container, "scan-root")
	child := filepath.Join(root, "child")
	outside := filepath.Join(container, "outside-target")
	path := filepath.Join(child, "record.txt")
	writeTestFile(t, path, []byte("scope escape payload"))

	report, err := NewEngine(1).SurfaceScan(context.Background(), []string{root}, nil)
	if err != nil {
		t.Fatalf("SurfaceScan returned error: %v", err)
	}
	if len(report.Files) != 1 {
		t.Fatalf("expected one record, got %#v", report.Files)
	}
	if err := os.Rename(child, outside); err != nil {
		t.Fatalf("move child outside root failed: %v", err)
	}
	createTestJunction(t, child, outside)

	err = ValidateRecordScope(report.Files[0])
	if !errors.Is(err, errReparsePoint) && !errors.Is(err, errScopeEscape) {
		t.Fatalf("ValidateRecordScope error = %v, want reparse/scope protection", err)
	}
}

func mustFileSnapshot(t *testing.T, path string) fileSnapshot {
	t.Helper()
	file, snapshot, err := openFileSnapshot(path)
	if err != nil {
		t.Fatalf("openFileSnapshot(%q) failed: %v", path, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close(%q) failed: %v", path, err)
	}
	return snapshot
}

func createTestJunction(t *testing.T, alias, target string) {
	t.Helper()
	output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", alias, target).CombinedOutput()
	if err != nil {
		t.Skipf("junction creation unavailable: %v (%s)", err, output)
	}
	t.Cleanup(func() { _ = os.Remove(alias) })
}
