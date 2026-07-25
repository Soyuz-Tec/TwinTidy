//go:build windows

package scanner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// scanForConsolidation scans root and returns the single expected duplicate
// group, so every consolidation test acts on real verified records.
func scanForConsolidation(t *testing.T, root string) DuplicateGroup {
	t.Helper()
	engine := NewEngine(2)
	groups, err := engine.Scan(context.Background(), []string{root}, nil)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected exactly one duplicate group, got %d", len(groups))
	}
	return groups[0]
}

// consolidationRequest marks every group member except the keeper chosen by
// the keep-rules engine, mirroring the production call path.
func consolidationRequest(t *testing.T, group DuplicateGroup) RecycleRequest {
	t.Helper()
	selections, err := ApplySelectionPolicy([]DuplicateGroup{group}, SelectionPolicy{Strategy: KeepNewest})
	if err != nil {
		t.Fatalf("ApplySelectionPolicy returned error: %v", err)
	}
	if len(selections) != 1 {
		t.Fatalf("expected one selection, got %d", len(selections))
	}
	return RecycleRequest{Group: group, Selected: selections[0].Marked}
}

// keptPath returns the group member that the request retains — the file the
// keep-rule chose, which is not necessarily the fixture's "keeper.txt".
func keptPath(t *testing.T, group DuplicateGroup, request RecycleRequest) string {
	t.Helper()
	selected := make(map[string]bool, len(request.Selected))
	for _, file := range request.Selected {
		selected[file.Path] = true
	}
	for _, member := range group.Files {
		if !selected[member.Path] {
			return member.Path
		}
	}
	t.Fatal("no kept member found in group")
	return ""
}

func identityOfPath(t *testing.T, path string) FileIdentity {
	t.Helper()
	file, snapshot, err := openFileSnapshot(path)
	if err != nil {
		t.Fatalf("openFileSnapshot(%q) failed: %v", path, err)
	}
	defer file.Close()
	return snapshot.identity
}

func assertNoLeftoverBackups(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".twintidy-consolidate-") {
			t.Fatalf("leftover backup file: %s", entry.Name())
		}
	}
}

func TestConsolidateReplacesDuplicateWithKeeperHardlink(t *testing.T) {
	root := userFileTestRoot(t)
	content := []byte(strings.Repeat("consolidate fixture payload\n", 64))
	writeTestFile(t, filepath.Join(root, "keeper.txt"), content)
	writeTestFile(t, filepath.Join(root, "copy.txt"), content)

	group := scanForConsolidation(t, root)
	request := consolidationRequest(t, group)
	result := ConsolidateExactDuplicates(context.Background(), request)

	if result.RequestError != "" {
		t.Fatalf("unexpected request error: %s", result.RequestError)
	}
	if len(result.Items) != 1 || result.Items[0].Status != HardlinkStatusConsolidated {
		t.Fatalf("expected one consolidated item, got %#v", result.Items)
	}

	kept := keptPath(t, group, request)
	target := request.Selected[0].Path
	if identityOfPath(t, kept) != identityOfPath(t, target) {
		t.Fatalf("kept and consolidated paths do not share one identity")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != string(content) {
		t.Fatalf("consolidated path content wrong: err=%v", err)
	}
	assertNoLeftoverBackups(t, root)
}

func TestConsolidateRefusesToMarkEveryMember(t *testing.T) {
	root := userFileTestRoot(t)
	content := []byte(strings.Repeat("invariant fixture payload\n", 64))
	writeTestFile(t, filepath.Join(root, "a.txt"), content)
	writeTestFile(t, filepath.Join(root, "b.txt"), content)

	group := scanForConsolidation(t, root)
	request := RecycleRequest{Group: group, Selected: group.Files}
	result := ConsolidateExactDuplicates(context.Background(), request)

	if result.RequestError == "" {
		t.Fatal("expected request rejection when every member is selected")
	}
	if identityOfPath(t, filepath.Join(root, "a.txt")) == identityOfPath(t, filepath.Join(root, "b.txt")) {
		t.Fatal("files were consolidated despite the rejected request")
	}
}

func TestConsolidateSkipsChangedCandidate(t *testing.T) {
	root := userFileTestRoot(t)
	content := []byte(strings.Repeat("changed fixture payload\n", 64))
	writeTestFile(t, filepath.Join(root, "keeper.txt"), content)
	writeTestFile(t, filepath.Join(root, "copy.txt"), content)

	group := scanForConsolidation(t, root)
	request := consolidationRequest(t, group)

	// Change the candidate after the scan, keeping the same size.
	mutated := append([]byte("X"), content[1:]...)
	target := request.Selected[0].Path
	if err := os.WriteFile(target, mutated, 0o600); err != nil {
		t.Fatalf("mutate candidate: %v", err)
	}

	result := ConsolidateExactDuplicates(context.Background(), request)
	if len(result.Items) != 1 || result.Items[0].Status != HardlinkStatusSkippedChanged {
		t.Fatalf("expected skipped-changed, got %#v", result.Items)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != string(mutated) {
		t.Fatalf("changed candidate must remain untouched: err=%v", err)
	}
}

func TestConsolidateSkipsPathSwappedCandidate(t *testing.T) {
	root := userFileTestRoot(t)
	content := []byte(strings.Repeat("swap fixture payload\n", 64))
	writeTestFile(t, filepath.Join(root, "keeper.txt"), content)
	writeTestFile(t, filepath.Join(root, "copy.txt"), content)

	group := scanForConsolidation(t, root)
	request := consolidationRequest(t, group)
	target := request.Selected[0].Path

	// Swap a different physical file into the candidate's path.
	swapped := filepath.Join(root, "swapped-in.txt")
	writeTestFile(t, swapped, content)
	if err := os.Rename(target, filepath.Join(root, "moved-away.txt")); err != nil {
		t.Fatalf("move original away: %v", err)
	}
	if err := os.Rename(swapped, target); err != nil {
		t.Fatalf("swap replacement in: %v", err)
	}

	result := ConsolidateExactDuplicates(context.Background(), request)
	if len(result.Items) != 1 || result.Items[0].Status != HardlinkStatusSkippedChanged {
		t.Fatalf("expected skipped-changed for path swap, got %#v", result.Items)
	}
	if identityOfPath(t, target) == identityOfPath(t, keptPath(t, group, request)) {
		t.Fatal("swapped-in object was wrongly consolidated")
	}
}

func TestConsolidateFailsClosedOnExclusivelyHeldCandidate(t *testing.T) {
	root := userFileTestRoot(t)
	content := []byte(strings.Repeat("held fixture payload\n", 64))
	writeTestFile(t, filepath.Join(root, "keeper.txt"), content)
	writeTestFile(t, filepath.Join(root, "copy.txt"), content)

	group := scanForConsolidation(t, root)
	request := consolidationRequest(t, group)
	target := request.Selected[0].Path

	// Hold the candidate open with no sharing at all.
	pathPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		t.Fatal(err)
	}
	holder, err := windows.CreateFile(pathPtr, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatalf("open exclusive holder: %v", err)
	}

	result := ConsolidateExactDuplicates(context.Background(), request)
	if err := windows.CloseHandle(holder); err != nil {
		t.Fatalf("close holder: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].Status == HardlinkStatusConsolidated {
		t.Fatalf("expected fail-closed result while candidate is held, got %#v", result.Items)
	}
	if identityOfPath(t, target) == identityOfPath(t, keptPath(t, group, request)) {
		t.Fatal("held candidate was wrongly consolidated")
	}
}

func TestConsolidateReadOnlyCandidateSucceedsAndCleansUp(t *testing.T) {
	root := userFileTestRoot(t)
	content := []byte(strings.Repeat("readonly fixture payload\n", 64))
	writeTestFile(t, filepath.Join(root, "keeper.txt"), content)
	writeTestFile(t, filepath.Join(root, "copy.txt"), content)

	group := scanForConsolidation(t, root)
	request := consolidationRequest(t, group)
	target := request.Selected[0].Path
	if err := os.Chmod(target, 0o400); err != nil {
		t.Fatalf("set read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(target, 0o600) })

	result := ConsolidateExactDuplicates(context.Background(), request)
	if len(result.Items) != 1 || result.Items[0].Status != HardlinkStatusConsolidated {
		t.Fatalf("expected consolidation of read-only candidate, got %#v", result.Items)
	}
	if identityOfPath(t, target) != identityOfPath(t, keptPath(t, group, request)) {
		t.Fatal("read-only candidate was not consolidated")
	}
	assertNoLeftoverBackups(t, root)
}

func TestConsolidateCancelledContextActsOnNothing(t *testing.T) {
	root := userFileTestRoot(t)
	content := []byte(strings.Repeat("cancel fixture payload\n", 64))
	writeTestFile(t, filepath.Join(root, "keeper.txt"), content)
	writeTestFile(t, filepath.Join(root, "copy.txt"), content)

	group := scanForConsolidation(t, root)
	request := consolidationRequest(t, group)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := ConsolidateExactDuplicates(ctx, request)
	if len(result.Items) != 1 || result.Items[0].Status != HardlinkStatusCancelled {
		t.Fatalf("expected cancelled item, got %#v", result.Items)
	}
	if identityOfPath(t, request.Selected[0].Path) == identityOfPath(t, keptPath(t, group, request)) {
		t.Fatal("cancelled request still consolidated the candidate")
	}
	assertNoLeftoverBackups(t, root)
}

func TestConsolidatedGroupDropsOutOfNextScan(t *testing.T) {
	root := userFileTestRoot(t)
	content := []byte(strings.Repeat("rescan fixture payload\n", 64))
	writeTestFile(t, filepath.Join(root, "keeper.txt"), content)
	writeTestFile(t, filepath.Join(root, "copy.txt"), content)

	group := scanForConsolidation(t, root)
	result := ConsolidateExactDuplicates(context.Background(), consolidationRequest(t, group))
	if result.Items[0].Status != HardlinkStatusConsolidated {
		t.Fatalf("consolidation failed: %#v", result.Items)
	}

	engine := NewEngine(2)
	groups, err := engine.Scan(context.Background(), []string{root}, nil)
	if err != nil {
		t.Fatalf("rescan returned error: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("consolidated files must not be re-reported, got %d group(s)", len(groups))
	}
}
