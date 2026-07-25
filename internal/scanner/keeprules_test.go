package scanner

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func keepRuleGroup(paths []string, modified []time.Time) DuplicateGroup {
	files := make([]FileRecord, len(paths))
	for index, path := range paths {
		files[index] = FileRecord{Path: path, Size: 1024, ModifiedAt: modified[index]}
	}
	return DuplicateGroup{Size: 1024, Hash: "test", Files: files}
}

func TestNormalizeSelectionPolicy(t *testing.T) {
	t.Parallel()

	if _, err := NormalizeSelectionPolicy(SelectionPolicy{Strategy: "keep-random"}); err == nil {
		t.Fatal("expected unknown strategy to be rejected")
	}

	policy, err := NormalizeSelectionPolicy(SelectionPolicy{PriorityFolders: []string{"", "  ", "relative"}})
	if err != nil {
		t.Fatalf("NormalizeSelectionPolicy returned error: %v", err)
	}
	if policy.Strategy != KeepNewest {
		t.Fatalf("expected empty strategy to default to KeepNewest, got %q", policy.Strategy)
	}
	if len(policy.PriorityFolders) != 1 || !filepath.IsAbs(policy.PriorityFolders[0]) {
		t.Fatalf("expected one absolute priority folder, got %v", policy.PriorityFolders)
	}
}

func TestApplySelectionPolicyKeepNewest(t *testing.T) {
	t.Parallel()

	base := filepath.Join("C:\\", "Users", "example")
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	group := keepRuleGroup(
		[]string{filepath.Join(base, "old.txt"), filepath.Join(base, "new.txt")},
		[]time.Time{older, newer},
	)

	selections, err := ApplySelectionPolicy([]DuplicateGroup{group}, SelectionPolicy{Strategy: KeepNewest})
	if err != nil {
		t.Fatalf("ApplySelectionPolicy returned error: %v", err)
	}
	if len(selections) != 1 {
		t.Fatalf("expected one selection, got %d", len(selections))
	}
	if filepath.Base(selections[0].Keep.Path) != "new.txt" {
		t.Fatalf("expected newest file kept, kept %q", selections[0].Keep.Path)
	}
	if len(selections[0].Marked) != 1 || filepath.Base(selections[0].Marked[0].Path) != "old.txt" {
		t.Fatalf("expected old.txt marked, got %v", selections[0].Marked)
	}
}

func TestApplySelectionPolicyKeepOldest(t *testing.T) {
	t.Parallel()

	base := filepath.Join("C:\\", "Users", "example")
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	group := keepRuleGroup(
		[]string{filepath.Join(base, "old.txt"), filepath.Join(base, "new.txt")},
		[]time.Time{older, older.Add(time.Hour)},
	)

	selections, err := ApplySelectionPolicy([]DuplicateGroup{group}, SelectionPolicy{Strategy: KeepOldest})
	if err != nil {
		t.Fatalf("ApplySelectionPolicy returned error: %v", err)
	}
	if filepath.Base(selections[0].Keep.Path) != "old.txt" {
		t.Fatalf("expected oldest file kept, kept %q", selections[0].Keep.Path)
	}
}

func TestApplySelectionPolicyFolderPriorityWins(t *testing.T) {
	t.Parallel()

	photos := filepath.Join("C:\\", "Photos")
	downloads := filepath.Join("C:\\", "Downloads")
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	group := keepRuleGroup(
		[]string{filepath.Join(downloads, "copy.jpg"), filepath.Join(photos, "original.jpg")},
		[]time.Time{older.Add(time.Hour), older},
	)

	selections, err := ApplySelectionPolicy([]DuplicateGroup{group}, SelectionPolicy{
		Strategy:        KeepNewest,
		PriorityFolders: []string{photos},
	})
	if err != nil {
		t.Fatalf("ApplySelectionPolicy returned error: %v", err)
	}
	if selections[0].Keep.Path != filepath.Join(photos, "original.jpg") {
		t.Fatalf("expected priority-folder file kept despite being older, kept %q", selections[0].Keep.Path)
	}
}

func TestApplySelectionPolicySkipsDegenerateGroups(t *testing.T) {
	t.Parallel()

	single := keepRuleGroup([]string{filepath.Join("C:\\", "only.txt")}, []time.Time{time.Now()})
	selections, err := ApplySelectionPolicy([]DuplicateGroup{single}, SelectionPolicy{})
	if err != nil {
		t.Fatalf("ApplySelectionPolicy returned error: %v", err)
	}
	if len(selections) != 0 {
		t.Fatalf("expected no selection for single-member group, got %d", len(selections))
	}
}

func TestValidateSelectionInvariantRejectsCorruptSelections(t *testing.T) {
	t.Parallel()

	base := filepath.Join("C:\\", "Users", "example")
	now := time.Now()
	group := keepRuleGroup(
		[]string{filepath.Join(base, "a.txt"), filepath.Join(base, "b.txt")},
		[]time.Time{now, now},
	)

	corrupt := []GroupSelection{
		{Group: group, Keep: FileRecord{Path: "C:\\intruder.txt"}, Marked: group.Files[:1]},
		{Group: group, Keep: group.Files[0], Marked: group.Files},
		{Group: group, Keep: group.Files[0], Marked: []FileRecord{group.Files[0]}},
	}
	for index, selection := range corrupt {
		if err := validateSelectionInvariant(selection); err == nil {
			t.Fatalf("case %d: expected invariant violation to be rejected", index)
		}
	}
}

// FuzzSelectionInvariant drives randomized group shapes and policies through
// the selection engine and asserts the one guarantee that must never break:
// a selection can never mark every member of a group.
func FuzzSelectionInvariant(f *testing.F) {
	f.Add(3, int64(1000), uint8(0), true)
	f.Add(2, int64(0), uint8(1), false)
	f.Add(9, int64(-50), uint8(2), true)

	f.Fuzz(func(t *testing.T, memberCount int, seed int64, strategyByte uint8, usePriority bool) {
		if memberCount < 0 || memberCount > 64 {
			t.Skip()
		}

		base := filepath.Join("C:\\", "fuzz")
		priority := filepath.Join(base, "priority")
		paths := make([]string, memberCount)
		modified := make([]time.Time, memberCount)
		epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		for index := range paths {
			folder := base
			if usePriority && index%3 == 0 {
				folder = priority
			}
			paths[index] = filepath.Join(folder, fmt.Sprintf("file-%d-%d.txt", index, seed))
			modified[index] = epoch.Add(time.Duration((seed+int64(index))%97) * time.Minute)
		}
		group := keepRuleGroup(paths, modified)

		strategy := KeepNewest
		if strategyByte%2 == 1 {
			strategy = KeepOldest
		}
		policy := SelectionPolicy{Strategy: strategy}
		if usePriority {
			policy.PriorityFolders = []string{priority}
		}

		selections, err := ApplySelectionPolicy([]DuplicateGroup{group}, policy)
		if err != nil {
			t.Fatalf("ApplySelectionPolicy returned error: %v", err)
		}
		if memberCount < 2 {
			if len(selections) != 0 {
				t.Fatalf("expected no selections for %d member(s)", memberCount)
			}
			return
		}
		if len(selections) != 1 {
			t.Fatalf("expected one selection, got %d", len(selections))
		}
		selection := selections[0]
		if len(selection.Marked) != memberCount-1 {
			t.Fatalf("expected %d marked, got %d", memberCount-1, len(selection.Marked))
		}
		for _, marked := range selection.Marked {
			if marked.Path == selection.Keep.Path {
				t.Fatalf("keeper %q is also marked", selection.Keep.Path)
			}
		}
	})
}
