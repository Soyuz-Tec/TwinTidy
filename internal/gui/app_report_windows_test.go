//go:build windows

package gui

import (
	"strings"
	"testing"
	"time"

	"github.com/Soyuz-Tec/duplicate-file-finder-go/internal/report"
	"github.com/Soyuz-Tec/duplicate-file-finder-go/internal/scanner"
)

func TestDuplicateGroupsFromRowsRebuildsGroupsInDisplayOrder(t *testing.T) {
	rows := []duplicateRow{
		{Group: 3, Hash: "cc", Duplicate: true, File: scanner.FileRecord{Path: `C:\c1`, Size: 30}},
		{Group: 3, Hash: "cc", Duplicate: true, File: scanner.FileRecord{Path: `C:\c2`, Size: 30}},
		{Group: 1, Hash: "aa", Duplicate: true, File: scanner.FileRecord{Path: `C:\a1`, Size: 10}},
		{Group: 1, Hash: "aa", Duplicate: true, File: scanner.FileRecord{Path: `C:\a2`, Size: 10}},
	}
	groups := duplicateGroupsFromRows(rows)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if groups[0].Hash != "cc" || len(groups[0].Files) != 2 || groups[0].Size != 30 {
		t.Fatalf("first group mismatch: %#v", groups[0])
	}
	if groups[1].Hash != "aa" || groups[1].Files[1].Path != `C:\a2` {
		t.Fatalf("second group mismatch: %#v", groups[1])
	}
}

func TestDuplicateGroupsFromRowsSkipsNonDuplicates(t *testing.T) {
	rows := []duplicateRow{
		{Group: 1, Hash: "", Duplicate: false, File: scanner.FileRecord{Path: `C:\surface-only`, Size: 5}},
	}
	if groups := duplicateGroupsFromRows(rows); len(groups) != 0 {
		t.Fatalf("non-duplicate rows produced groups: %#v", groups)
	}
	if groups := duplicateGroupsFromRows(nil); len(groups) != 0 {
		t.Fatalf("nil rows produced groups: %#v", groups)
	}
}

func TestReportBytesForPathSelectsFormatByExtension(t *testing.T) {
	document := report.BuildDocument(`C:\scanned`, []scanner.DuplicateGroup{
		{Size: 10, Hash: "aa", Files: []scanner.FileRecord{{Path: `C:\a1`, Size: 10}, {Path: `C:\a2`, Size: 10}}},
	}, time.Now())

	jsonData, err := reportBytesForPath(`C:\out\report.JSON`, document)
	if err != nil {
		t.Fatalf("JSON serialization failed: %v", err)
	}
	if !strings.HasPrefix(string(jsonData), "{") {
		t.Fatalf("JSON output does not look like JSON: %q", jsonData[:1])
	}

	csvData, err := reportBytesForPath(`C:\out\report.csv`, document)
	if err != nil {
		t.Fatalf("CSV serialization failed: %v", err)
	}
	if !strings.HasPrefix(string(csvData), "generatedAt,scanFolder,group,sha256") {
		t.Fatalf("CSV output missing header: %q", string(csvData[:20]))
	}

	fallback, err := reportBytesForPath(`C:\out\no-extension`, document)
	if err != nil {
		t.Fatalf("fallback serialization failed: %v", err)
	}
	if string(fallback[:11]) != "generatedAt" {
		t.Fatal("extension-less path did not fall back to CSV")
	}
}

func TestDefaultReportFileName(t *testing.T) {
	name := defaultReportFileName(time.Date(2026, 7, 15, 9, 5, 4, 0, time.UTC))
	if name != "TwinTidy-duplicates-20260715-090504.csv" {
		t.Fatalf("defaultReportFileName = %q", name)
	}
}
