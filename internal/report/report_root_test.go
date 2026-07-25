package report

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Soyuz-Tec/twintidy/internal/scanner"
)

// crossRootGroups builds one duplicate group whose members were discovered
// by two different scan roots, as a multi-folder scan produces.
func crossRootGroups() []scanner.DuplicateGroup {
	rootA := filepath.Join("C:\\", "Main", "Photos")
	rootB := filepath.Join("E:\\", "Backup", "Photos")
	return []scanner.DuplicateGroup{{
		Size: 2048,
		Hash: "feedface",
		Files: []scanner.FileRecord{
			{
				Path:  filepath.Join(rootA, "holiday.jpg"),
				Size:  2048,
				Scope: scanner.AuthorizedScope{RootFinalPath: rootA},
			},
			{
				Path:  filepath.Join(rootB, "holiday.jpg"),
				Size:  2048,
				Scope: scanner.AuthorizedScope{RootFinalPath: rootB},
			},
		},
	}}
}

func TestJSONReportTagsFileRoots(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	_, err := Write(context.Background(), &out, FormatJSON, "multi", crossRootGroups(), time.Now())
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	var document Document
	if err := json.Unmarshal(out.Bytes(), &document); err != nil {
		t.Fatalf("report is not valid JSON: %v", err)
	}
	if len(document.Groups) != 1 || len(document.Groups[0].Files) != 2 {
		t.Fatalf("unexpected report shape: %+v", document)
	}
	roots := map[string]bool{}
	for _, file := range document.Groups[0].Files {
		if file.Root == "" {
			t.Fatalf("file %q has no root tag", file.Path)
		}
		roots[file.Root] = true
	}
	if len(roots) != 2 {
		t.Fatalf("expected two distinct roots in cross-root group, got %v", roots)
	}
}

func TestCSVReportTagsFileRoots(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	_, err := Write(context.Background(), &out, FormatCSV, "multi", crossRootGroups(), time.Now())
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	records, err := csv.NewReader(strings.NewReader(out.String())).ReadAll()
	if err != nil {
		t.Fatalf("report is not valid CSV: %v", err)
	}
	header := records[0]
	rootColumn := -1
	for index, name := range header {
		if name == "root" {
			rootColumn = index
		}
	}
	if rootColumn == -1 {
		t.Fatalf("csv header missing root column: %v", header)
	}
	if len(records) != 3 {
		t.Fatalf("expected header plus two rows, got %d", len(records))
	}
	if records[1][rootColumn] == records[2][rootColumn] {
		t.Fatalf("expected distinct roots per member, got %q for both", records[1][rootColumn])
	}
	for _, row := range records[1:] {
		if row[rootColumn] == "" {
			t.Fatalf("row missing root value: %v", row)
		}
	}
}
