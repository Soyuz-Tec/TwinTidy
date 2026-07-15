package report

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Soyuz-Tec/duplicate-file-finder-go/internal/scanner"
)

func sampleGroups() []scanner.DuplicateGroup {
	modified := time.Date(2026, 7, 1, 10, 30, 0, 0, time.UTC)
	created := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	return []scanner.DuplicateGroup{
		{
			Size: 2048,
			Hash: "aa11",
			Files: []scanner.FileRecord{
				{Path: `C:\docs\report.pdf`, Size: 2048, CreatedAt: created, ModifiedAt: modified, Category: scanner.CategoryPDF},
				{Path: `C:\backup\report copy.pdf`, Size: 2048, CreatedAt: created, ModifiedAt: modified, Category: scanner.CategoryPDF},
				{Path: `C:\backup\report copy 2.pdf`, Size: 2048, CreatedAt: created, ModifiedAt: modified, Category: scanner.CategoryPDF},
			},
		},
		{
			Size: 100,
			Hash: "bb22",
			Files: []scanner.FileRecord{
				{Path: `=SUM(A1:A9).txt`, Size: 100, ModifiedAt: modified, Category: scanner.CategoryText},
				{Path: `C:\notes\copy.txt`, Size: 100, ModifiedAt: modified, Category: scanner.CategoryText},
			},
		},
	}
}

func TestBuildDocumentCountsAndEstimate(t *testing.T) {
	document := BuildDocument(`C:\scanned`, sampleGroups(), time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC))
	if document.Schema != Schema {
		t.Fatalf("schema = %q", document.Schema)
	}
	if document.GeneratedAt != "2026-07-15T12:00:00Z" {
		t.Fatalf("generatedAt = %q", document.GeneratedAt)
	}
	if document.GroupCount != 2 || document.FileCount != 5 {
		t.Fatalf("counts = %d groups, %d files", document.GroupCount, document.FileCount)
	}
	// Keeping one copy per group: 2 extra PDFs at 2048 plus 1 extra text at 100.
	if document.ReclaimableBytes != 2*2048+100 {
		t.Fatalf("reclaimableBytes = %d", document.ReclaimableBytes)
	}
}

func TestBuildDocumentEmpty(t *testing.T) {
	document := BuildDocument("", nil, time.Unix(0, 0))
	if document.GroupCount != 0 || document.FileCount != 0 || document.ReclaimableBytes != 0 {
		t.Fatalf("empty document has non-zero counts: %#v", document)
	}
	if len(document.Groups) != 0 {
		t.Fatalf("empty document has groups: %#v", document.Groups)
	}
}

func TestJSONDocumentRoundTrips(t *testing.T) {
	document := BuildDocument(`C:\scanned`, sampleGroups(), time.Now())
	data, err := document.MarshalJSONDocument()
	if err != nil {
		t.Fatalf("MarshalJSONDocument failed: %v", err)
	}
	if data[len(data)-1] != '\n' {
		t.Fatal("JSON document does not end with a newline")
	}

	var decoded Document
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if decoded.Schema != Schema || decoded.GroupCount != 2 || len(decoded.Groups) != 2 {
		t.Fatalf("decoded document mismatch: %#v", decoded)
	}
	if decoded.Groups[0].Files[0].Path != `C:\docs\report.pdf` {
		t.Fatalf("decoded path mismatch: %q", decoded.Groups[0].Files[0].Path)
	}
}

func TestCSVGuardsFormulaInjection(t *testing.T) {
	document := BuildDocument(`C:\scanned`, sampleGroups(), time.Now())
	data, err := document.MarshalCSV()
	if err != nil {
		t.Fatalf("MarshalCSV failed: %v", err)
	}
	text := string(data)

	lines := strings.Split(strings.TrimRight(text, "\r\n"), "\r\n")
	if len(lines) != 1+5 {
		t.Fatalf("expected header plus 5 rows, got %d lines", len(lines))
	}
	if lines[0] != "group,sha256,path,size,createdAt,modifiedAt,category" {
		t.Fatalf("header = %q", lines[0])
	}
	if !strings.Contains(text, `,'=SUM(A1:A9).txt,`) {
		t.Fatal("formula-leading path was not neutralized")
	}
	if strings.Contains(text, "\n=") || strings.Contains(text, ",=") {
		t.Fatal("a cell still begins with a raw formula character")
	}
}

func TestGuardSpreadsheetFormula(t *testing.T) {
	cases := map[string]string{
		"":                 "",
		"plain.txt":        "plain.txt",
		"=cmd":             "'=cmd",
		"+sum":             "'+sum",
		"-neg":             "'-neg",
		"@ref":             "'@ref",
		"\tlead":           "'\tlead",
		`C:\normal\path.p`: `C:\normal\path.p`,
	}
	for input, expected := range cases {
		if got := guardSpreadsheetFormula(input); got != expected {
			t.Fatalf("guardSpreadsheetFormula(%q) = %q, want %q", input, got, expected)
		}
	}
}
