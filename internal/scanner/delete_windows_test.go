//go:build windows

package scanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestRecyclePolicyHoldsKeeperStableAcrossAdapterCall(t *testing.T) {
	fixture := newRecycleFixture(t, 2)
	target := fixture.group.Files[0]
	keeper := fixture.group.Files[1]
	keeperDeleteBlocked := false
	adapter := &fakeRecycleAdapter{recycle: func(_ context.Context, path string) error {
		if err := os.Remove(keeper.Path); err == nil {
			return errors.New("keeper was deletable while native adapter ran")
		}
		if err := os.Rename(keeper.Path, keeper.Path+".renamed"); err == nil {
			return errors.New("keeper was renameable while native adapter ran")
		}
		if err := os.WriteFile(keeper.Path, []byte("mutation"), 0o600); err == nil {
			return errors.New("keeper was writable while native adapter ran")
		}
		keeperDeleteBlocked = true
		return os.Remove(path)
	}}

	result := recycleExactDuplicates(context.Background(), recycleRequest(fixture.group, target), adapter)
	assertSingleRecycleStatus(t, result, RecycleStatusRecycled)
	if !keeperDeleteBlocked {
		t.Fatal("adapter did not observe keeper delete sharing protection")
	}
	if _, err := os.Stat(keeper.Path); err != nil {
		t.Fatalf("keeper disappeared: %v", err)
	}
}

func TestRecyclePolicyProtectsHardLinkAddedAfterScan(t *testing.T) {
	fixture := newRecycleFixture(t, 2)
	target := fixture.group.Files[0]
	link := filepath.Join(fixture.root, "late-hard-link.txt")
	if err := os.Link(target.Path, link); err != nil {
		t.Skipf("hard links unavailable on test volume: %v", err)
	}
	adapter := &fakeRecycleAdapter{recycle: removeRecycledTestFile}

	result := recycleExactDuplicates(context.Background(), recycleRequest(fixture.group, target), adapter)
	assertSingleRecycleStatus(t, result, RecycleStatusSkippedProtected)
	if len(adapter.calls) != 0 {
		t.Fatalf("adapter called for hard-linked target: %v", adapter.calls)
	}
}

func TestRecyclePolicyProtectsNamedStreamAddedAfterScan(t *testing.T) {
	fixture := newRecycleFixture(t, 2)
	target := fixture.group.Files[0]
	streamPath := target.Path + ":TwinTidyLateStream"
	if err := os.WriteFile(streamPath, []byte("unique stream"), 0o644); err != nil {
		t.Skipf("alternate data streams unavailable on test volume: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(streamPath) })
	adapter := &fakeRecycleAdapter{recycle: removeRecycledTestFile}

	result := recycleExactDuplicates(context.Background(), recycleRequest(fixture.group, target), adapter)
	assertSingleRecycleStatus(t, result, RecycleStatusSkippedProtected)
	if len(adapter.calls) != 0 {
		t.Fatalf("adapter called for target with named stream: %v", adapter.calls)
	}
}

func TestRecyclePolicyProtectsTargetReplacedByJunction(t *testing.T) {
	fixture := newRecycleFixture(t, 2)
	target := fixture.group.Files[0]
	backup := target.Path + ".original"
	junctionTarget := filepath.Join(fixture.root, "junction-target")
	if err := os.Rename(target.Path, backup); err != nil {
		t.Fatalf("Rename target failed: %v", err)
	}
	if err := os.MkdirAll(junctionTarget, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	createTestJunction(t, target.Path, junctionTarget)
	adapter := &fakeRecycleAdapter{recycle: removeRecycledTestFile}

	result := recycleExactDuplicates(context.Background(), recycleRequest(fixture.group, target), adapter)
	assertSingleRecycleStatus(t, result, RecycleStatusSkippedProtected)
	if len(adapter.calls) != 0 {
		t.Fatalf("adapter called for junction target: %v", adapter.calls)
	}
}

func TestRecyclePolicyRejectsSelectedRootReplacement(t *testing.T) {
	fixture := newRecycleFixture(t, 2)
	target := fixture.group.Files[0]
	originalRoot := fixture.root + ".original"
	t.Cleanup(func() { _ = os.RemoveAll(originalRoot) })
	if err := os.Rename(fixture.root, originalRoot); err != nil {
		t.Fatalf("Rename root failed: %v", err)
	}
	if err := os.MkdirAll(fixture.root, 0o755); err != nil {
		t.Fatalf("recreate root failed: %v", err)
	}
	for _, record := range fixture.group.Files {
		writeTestFile(t, record.Path, fixture.content)
	}

	adapter := &fakeRecycleAdapter{recycle: removeRecycledTestFile}
	result := recycleExactDuplicates(context.Background(), recycleRequest(fixture.group, target), adapter)
	assertSingleRecycleStatus(t, result, RecycleStatusFailed)
	if len(adapter.calls) != 0 {
		t.Fatalf("adapter called after selected-root replacement: %v", adapter.calls)
	}
}

func TestRecyclePolicyRejectsNestedJunctionSwap(t *testing.T) {
	root := userFileTestRoot(t)
	child := filepath.Join(root, "child")
	outsideRoot := root + "-outside"
	t.Cleanup(func() { _ = os.RemoveAll(outsideRoot) })
	outside := filepath.Join(outsideRoot, "moved-child")
	content := []byte("TwinTidy scoped duplicate payload")
	writeTestFile(t, filepath.Join(child, "copy-01.txt"), content)
	writeTestFile(t, filepath.Join(child, "copy-02.txt"), content)
	groups, err := NewEngine(1).Scan(context.Background(), []string{root}, nil)
	if err != nil {
		t.Fatalf("Scan fixture failed: %v", err)
	}
	if len(groups) != 1 || len(groups[0].Files) != 2 {
		t.Fatalf("unexpected fixture groups: %#v", groups)
	}
	if err := os.MkdirAll(filepath.Dir(outside), 0o755); err != nil {
		t.Fatalf("create outside parent failed: %v", err)
	}
	if err := os.Rename(child, outside); err != nil {
		t.Fatalf("move child outside root failed: %v", err)
	}
	createTestJunction(t, child, outside)

	adapter := &fakeRecycleAdapter{recycle: removeRecycledTestFile}
	result := recycleExactDuplicates(context.Background(), recycleRequest(groups[0], groups[0].Files[0]), adapter)
	assertSingleRecycleStatus(t, result, RecycleStatusSkippedProtected)
	if len(adapter.calls) != 0 {
		t.Fatalf("adapter called after nested-junction swap: %v", adapter.calls)
	}
}

func TestRecyclePolicyFailsClosedForLockedTarget(t *testing.T) {
	fixture := newRecycleFixture(t, 2)
	target := fixture.group.Files[0]
	pathPtr, err := windows.UTF16PtrFromString(target.Path)
	if err != nil {
		t.Fatalf("UTF16PtrFromString failed: %v", err)
	}
	handle, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_WRITE,
		windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatalf("lock target failed: %v", err)
	}
	defer windows.CloseHandle(handle)
	adapter := &fakeRecycleAdapter{recycle: removeRecycledTestFile}

	result := recycleExactDuplicates(context.Background(), recycleRequest(fixture.group, target), adapter)
	assertSingleRecycleStatus(t, result, RecycleStatusFailed)
	if len(adapter.calls) != 0 {
		t.Fatalf("adapter called for locked target: %v", adapter.calls)
	}
}

func TestProductionRecycleAdapterFailsClosedWithoutChangingFiles(t *testing.T) {
	fixture := newRecycleFixture(t, 2)
	target := fixture.group.Files[0]
	keeper := fixture.group.Files[1]
	targetBefore := mustFileSnapshot(t, target.Path)
	keeperBefore := mustFileSnapshot(t, keeper.Path)

	if RecycleSupported() {
		t.Fatal("production recycle unexpectedly reports support")
	}
	result := RecycleExactDuplicates(context.Background(), recycleRequest(fixture.group, target))
	assertSingleRecycleStatus(t, result, RecycleStatusFailed)
	if result.Items[0].Reason != errRecycleUnsupported.Error() {
		t.Fatalf("failure reason = %q, want %q", result.Items[0].Reason, errRecycleUnsupported)
	}
	if after := mustFileSnapshot(t, target.Path); !snapshotsEqual(targetBefore, after) {
		t.Fatalf("unsupported recycle changed target: before=%#v after=%#v", targetBefore, after)
	}
	if after := mustFileSnapshot(t, keeper.Path); !snapshotsEqual(keeperBefore, after) {
		t.Fatalf("unsupported recycle changed keeper: before=%#v after=%#v", keeperBefore, after)
	}
}
