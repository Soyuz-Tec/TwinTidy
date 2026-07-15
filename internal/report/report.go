// Package report serializes verified duplicate-scan results into local CSV
// and JSON documents. A report is a read-only record of what the user
// reviewed: it grants no authority, is written only where the user chooses,
// and never leaves the computer on its own.
package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strconv"
	"time"

	"github.com/Soyuz-Tec/duplicate-file-finder-go/internal/scanner"
)

// Schema identifies the JSON document layout.
const Schema = "twintidy.duplicate-report/v1"

// File is one member of an exported duplicate group.
type File struct {
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	CreatedAt  string `json:"createdAt,omitempty"`
	ModifiedAt string `json:"modifiedAt,omitempty"`
	Category   string `json:"category,omitempty"`
}

// Group is one verified duplicate group: files whose complete SHA-256
// content matched at scan time.
type Group struct {
	Size  int64  `json:"size"`
	Hash  string `json:"sha256"`
	Files []File `json:"files"`
}

// Document is the exported report.
type Document struct {
	Schema           string  `json:"schema"`
	GeneratedAt      string  `json:"generatedAt"`
	Folder           string  `json:"folder,omitempty"`
	GroupCount       int     `json:"groupCount"`
	FileCount        int     `json:"fileCount"`
	ReclaimableBytes int64   `json:"reclaimableBytes"`
	Groups           []Group `json:"groups"`
}

// BuildDocument converts scan results into a Document. Group and file order
// is preserved so the export matches what the user reviewed on screen.
// ReclaimableBytes is the planning estimate of keeping one copy per group;
// it asserts nothing about what any future cleanup would actually do.
func BuildDocument(folder string, groups []scanner.DuplicateGroup, generatedAt time.Time) Document {
	document := Document{
		Schema:      Schema,
		GeneratedAt: generatedAt.UTC().Format(time.RFC3339),
		Folder:      folder,
		Groups:      make([]Group, 0, len(groups)),
	}
	for _, group := range groups {
		exported := Group{
			Size:  group.Size,
			Hash:  group.Hash,
			Files: make([]File, 0, len(group.Files)),
		}
		for _, file := range group.Files {
			exported.Files = append(exported.Files, File{
				Path:       file.Path,
				Size:       file.Size,
				CreatedAt:  formatTimestamp(file.CreatedAt),
				ModifiedAt: formatTimestamp(file.ModifiedAt),
				Category:   string(file.Category),
			})
		}
		document.Groups = append(document.Groups, exported)
		document.FileCount += len(exported.Files)
		if extra := int64(len(exported.Files)) - 1; extra > 0 {
			document.ReclaimableBytes += extra * group.Size
		}
	}
	document.GroupCount = len(document.Groups)
	return document
}

// MarshalJSONDocument renders the canonical indented JSON form.
func (d Document) MarshalJSONDocument() ([]byte, error) {
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// MarshalCSV renders one row per exported file. Cell values that a
// spreadsheet would evaluate as formulas are prefixed with an apostrophe so
// an adversarial file name cannot execute when the report is opened.
func (d Document) MarshalCSV() ([]byte, error) {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	writer.UseCRLF = true

	if err := writer.Write([]string{"group", "sha256", "path", "size", "createdAt", "modifiedAt", "category"}); err != nil {
		return nil, err
	}
	for index, group := range d.Groups {
		for _, file := range group.Files {
			row := []string{
				strconv.Itoa(index + 1),
				group.Hash,
				guardSpreadsheetFormula(file.Path),
				strconv.FormatInt(file.Size, 10),
				file.CreatedAt,
				file.ModifiedAt,
				guardSpreadsheetFormula(file.Category),
			}
			if err := writer.Write(row); err != nil {
				return nil, err
			}
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func formatTimestamp(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

// guardSpreadsheetFormula neutralizes cells that Excel-compatible software
// would interpret as formulas or command triggers.
func guardSpreadsheetFormula(value string) string {
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + value
	}
	return value
}
