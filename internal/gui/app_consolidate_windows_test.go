//go:build windows

package gui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Soyuz-Tec/twintidy/internal/scanner"
)

func consolidateTestPlan(t *testing.T) recyclePlan {
	t.Helper()
	base := filepath.Join("C:\\", "Users", "example", "Documents")
	keep := scanner.FileRecord{Path: filepath.Join(base, "keep.txt"), Size: 4}
	link := scanner.FileRecord{Path: filepath.Join(base, "link.txt"), Size: 4}
	return recyclePlan{
		selectedCount: 1,
		groups: []plannedRecycleGroup{{
			groupNumber: 1,
			request: scanner.RecycleRequest{
				Group: scanner.DuplicateGroup{
					Size:  4,
					Hash:  "cafe",
					Files: []scanner.FileRecord{keep, link},
				},
				Selected: []scanner.FileRecord{link},
			},
		}},
	}
}

func TestConsolidateConfirmationNamesSharedEditCaveat(t *testing.T) {
	t.Parallel()

	message, err := consolidateConfirmationMessage(consolidateTestPlan(t))
	if err != nil {
		t.Fatalf("consolidateConfirmationMessage returned error: %v", err)
	}
	for _, required := range []string{
		"ONE shared file",
		"Editing the file at any of those paths changes it at all of them",
		"Deleting one path does not free space",
		"LINK: ",
		"KEEP: ",
		"link.txt",
		"keep.txt",
	} {
		if !strings.Contains(message, required) {
			t.Fatalf("confirmation is missing %q:\n%s", required, message)
		}
	}
}

func TestConsolidateConfirmationRejectsOversizedBatch(t *testing.T) {
	t.Parallel()

	base := filepath.Join("C:\\", "Users", "example", "Documents")
	files := make([]scanner.FileRecord, 0, maxRecycleConfirmationEntries+2)
	for index := 0; index < maxRecycleConfirmationEntries+2; index++ {
		files = append(files, scanner.FileRecord{
			Path: filepath.Join(base, strings.Repeat("x", 6)+"-"+strings.Repeat("y", index%7)+".txt"),
			Size: 4,
		})
	}
	plan := recyclePlan{
		selectedCount: len(files) - 1,
		groups: []plannedRecycleGroup{{
			groupNumber: 1,
			request: scanner.RecycleRequest{
				Group:    scanner.DuplicateGroup{Size: 4, Hash: "cafe", Files: files},
				Selected: files[1:],
			},
		}},
	}
	if _, err := consolidateConfirmationMessage(plan); err == nil {
		t.Fatal("expected oversized confirmation to be rejected")
	}
}

func TestApplyConsolidateBatchToModelAnnotatesWithoutRemoving(t *testing.T) {
	t.Parallel()

	base := filepath.Join("C:\\", "Users", "example", "Documents")
	keepPath := filepath.Join(base, "keep.txt")
	linkPath := filepath.Join(base, "link.txt")
	model := &duplicateTableModel{rows: []duplicateRow{
		{File: scanner.FileRecord{Path: keepPath}, Duplicate: true, Selected: false},
		{File: scanner.FileRecord{Path: linkPath}, Duplicate: true, Selected: true},
	}}

	batch := consolidateBatchResult{groups: []consolidateGroupResult{{
		groupNumber: 1,
		result: scanner.HardlinkResult{Items: []scanner.HardlinkItemResult{{
			Path:   linkPath,
			Status: scanner.HardlinkStatusConsolidated,
		}}},
	}}}

	outcome := applyConsolidateBatchToModel(model, batch)
	if outcome.consolidated != 1 {
		t.Fatalf("expected one consolidated item, got %+v", outcome)
	}
	if len(model.rows) != 2 {
		t.Fatalf("consolidation must not remove rows, got %d", len(model.rows))
	}
	for _, row := range model.rows {
		if row.Selected {
			t.Fatalf("all checkboxes must clear, row %q still selected", row.File.Path)
		}
	}
	if model.rows[1].Status != "Consolidated - shared copy" {
		t.Fatalf("expected consolidated status, got %q", model.rows[1].Status)
	}
	if model.rows[0].Status == "Consolidated - shared copy" {
		t.Fatal("keeper row must not be marked consolidated")
	}
}

func TestConsolidateIssueSummaryListsBackups(t *testing.T) {
	t.Parallel()

	backup := filepath.Join("C:\\", "Users", "example", ".twintidy-consolidate-abc.bak")
	outcome := consolidateOutcome{failed: 1, backupPaths: []string{backup}}
	if !consolidateOutcomeHasIssues(outcome) {
		t.Fatal("expected outcome with a backup to report issues")
	}
	summary := consolidateIssueSummary(outcome)
	if !strings.Contains(summary, "No content was lost") {
		t.Fatalf("summary must state content safety:\n%s", summary)
	}
	if !strings.Contains(summary, ".twintidy-consolidate-abc.bak") {
		t.Fatalf("summary must list the backup path:\n%s", summary)
	}
}

func TestConsolidateOutcomeSummaryCounts(t *testing.T) {
	t.Parallel()

	summary := consolidateOutcomeSummary(consolidateOutcome{consolidated: 3, crossVolume: 1, failed: 1})
	if !strings.Contains(summary, "Consolidated 3 file(s)") || !strings.Contains(summary, "2 file(s) were kept unchanged") {
		t.Fatalf("unexpected summary: %s", summary)
	}
	clean := consolidateOutcome{consolidated: 2}
	if consolidateOutcomeHasIssues(clean) {
		t.Fatal("clean outcome must not report issues")
	}
}
