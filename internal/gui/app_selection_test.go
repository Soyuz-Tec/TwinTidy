package gui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Soyuz-Tec/twintidy/internal/scanner"
)

// selectionModel builds a table model with two duplicate groups whose
// members differ in modification time, plus a non-duplicate row that must
// never be selected.
func selectionModel() *duplicateTableModel {
	base := filepath.Join("C:\\", "Users", "example")
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	row := func(name, key, hash string, offset time.Duration, duplicate bool) duplicateRow {
		return duplicateRow{
			Key:       key,
			Hash:      hash,
			Duplicate: duplicate,
			File: scanner.FileRecord{
				Path:       filepath.Join(base, name),
				Size:       1024,
				ModifiedAt: epoch.Add(offset),
			},
		}
	}
	return &duplicateTableModel{rows: []duplicateRow{
		row("a-old.txt", "g1", "hash1", 0, true),
		row("a-new.txt", "g1", "hash1", 2*time.Hour, true),
		row("a-mid.txt", "g1", "hash1", time.Hour, true),
		row("b-new.txt", "g2", "hash2", 3*time.Hour, true),
		row("b-old.txt", "g2", "hash2", time.Hour, true),
		row("unique.txt", "g3", "hash3", 0, false),
	}}
}

func selectedNames(model *duplicateTableModel) map[string]bool {
	names := make(map[string]bool)
	for _, row := range model.rows {
		if row.Selected {
			names[filepath.Base(row.File.Path)] = true
		}
	}
	return names
}

func TestSelectAllExceptNewestKeepsTheNewestPerGroup(t *testing.T) {
	model := selectionModel()
	model.selectAllExcept(true)

	selected := selectedNames(model)
	if selected["a-new.txt"] || selected["b-new.txt"] {
		t.Fatalf("the newest file in a group must stay unselected: %v", selected)
	}
	for _, name := range []string{"a-old.txt", "a-mid.txt", "b-old.txt"} {
		if !selected[name] {
			t.Fatalf("expected %s to be selected: %v", name, selected)
		}
	}
	if selected["unique.txt"] {
		t.Fatal("a non-duplicate row must never be selected")
	}
}

func TestSelectAllExceptOldestKeepsTheOldestPerGroup(t *testing.T) {
	model := selectionModel()
	model.selectAllExcept(false)

	selected := selectedNames(model)
	if selected["a-old.txt"] || selected["b-old.txt"] {
		t.Fatalf("the oldest file in a group must stay unselected: %v", selected)
	}
	if !selected["a-new.txt"] || !selected["b-new.txt"] {
		t.Fatalf("expected the newer files to be selected: %v", selected)
	}
}

// The invariant that matters: whatever the rule, every group keeps at least
// one unselected copy. The engine enforces this, and this test proves the UI
// path inherits that guarantee rather than reimplementing it.
func TestSelectionAlwaysLeavesOneCopyPerGroup(t *testing.T) {
	for _, keepNewest := range []bool{true, false} {
		model := selectionModel()
		model.selectAllExcept(keepNewest)

		remaining := make(map[string]int)
		total := make(map[string]int)
		for _, row := range model.rows {
			if !row.Duplicate {
				continue
			}
			total[row.Key]++
			if !row.Selected {
				remaining[row.Key]++
			}
		}
		for key, count := range total {
			if remaining[key] < 1 {
				t.Fatalf("keepNewest=%v: group %s has no unselected copy of %d", keepNewest, key, count)
			}
			if remaining[key] != 1 {
				t.Fatalf("keepNewest=%v: group %s kept %d copies, want exactly 1", keepNewest, key, remaining[key])
			}
		}
	}
}

// A priority folder outranks the timestamp rule, so a copy in a preferred
// location survives even when it is not the newest.
func TestSelectionHonoursPriorityFolders(t *testing.T) {
	photos := filepath.Join("C:\\", "Photos")
	downloads := filepath.Join("C:\\", "Downloads")
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	model := &duplicateTableModel{rows: []duplicateRow{
		{
			Key: "g1", Hash: "hash1", Duplicate: true,
			File: scanner.FileRecord{
				Path: filepath.Join(downloads, "copy.jpg"), Size: 2048,
				ModifiedAt: epoch.Add(time.Hour), // newer
			},
		},
		{
			Key: "g1", Hash: "hash1", Duplicate: true,
			File: scanner.FileRecord{
				Path: filepath.Join(photos, "original.jpg"), Size: 2048,
				ModifiedAt: epoch, // older, but in the priority folder
			},
		},
	}}

	model.applySelectionPolicy(scanner.SelectionPolicy{
		Strategy:        scanner.KeepNewest,
		PriorityFolders: []string{photos},
	})

	for _, row := range model.rows {
		inPhotos := filepath.Dir(row.File.Path) == photos
		if inPhotos && row.Selected {
			t.Fatal("the priority-folder copy must be kept even though it is older")
		}
		if !inPhotos && !row.Selected {
			t.Fatal("the non-priority copy should have been selected")
		}
	}
}
