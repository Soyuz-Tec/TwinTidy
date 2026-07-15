//go:build windows

package gui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/lxn/walk"

	"github.com/Soyuz-Tec/duplicate-file-finder-go/internal/diagnostics"
	"github.com/Soyuz-Tec/duplicate-file-finder-go/internal/report"
	"github.com/Soyuz-Tec/duplicate-file-finder-go/internal/scanner"
)

// duplicateGroupsFromRows rebuilds the verified duplicate groups from the
// rows currently shown, so an export always matches what the user reviewed.
func duplicateGroupsFromRows(rows []duplicateRow) []scanner.DuplicateGroup {
	var groups []scanner.DuplicateGroup
	indexByGroup := map[int]int{}
	for _, row := range rows {
		if !row.Duplicate {
			continue
		}
		index, seen := indexByGroup[row.Group]
		if !seen {
			index = len(groups)
			indexByGroup[row.Group] = index
			groups = append(groups, scanner.DuplicateGroup{
				Size: row.File.Size,
				Hash: row.Hash,
			})
		}
		groups[index].Files = append(groups[index].Files, row.File)
	}
	return groups
}

func defaultReportFileName(now time.Time) string {
	return "TwinTidy-duplicates-" + now.Format("20060102-150405") + ".csv"
}

// reportBytesForPath serializes the document in the format implied by the
// chosen file extension; anything that is not .json exports as CSV.
func reportBytesForPath(path string, document report.Document) ([]byte, error) {
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return document.MarshalJSONDocument()
	}
	return document.MarshalCSV()
}

func (a *windowsApp) exportReport() {
	if a.operation.phase != phaseResultsReady {
		return
	}
	groups := duplicateGroupsFromRows(a.model.rows)
	if len(groups) == 0 {
		return
	}

	dialog := walk.FileDialog{
		Title:    "Export duplicate report",
		Filter:   "CSV report (*.csv)|*.csv|JSON report (*.json)|*.json",
		FilePath: defaultReportFileName(time.Now()),
	}
	accepted, err := dialog.ShowSave(a.mw)
	if err != nil {
		walk.MsgBox(a.mw, "Export Failed", displayUntrustedText(err.Error()), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	if !accepted || dialog.FilePath == "" {
		return
	}

	path := dialog.FilePath
	if filepath.Ext(path) == "" {
		if dialog.FilterIndex == 2 {
			path += ".json"
		} else {
			path += ".csv"
		}
	}

	document := report.BuildDocument(a.operation.folder, groups, time.Now())
	data, err := reportBytesForPath(path, document)
	if err != nil {
		diagnostics.Logf("report serialization failed: error_type=%T", err)
		walk.MsgBox(a.mw, "Export Failed", displayUntrustedText(err.Error()), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	if err := report.WriteFileAtomic(path, data); err != nil {
		diagnostics.Logf("report write failed: error_type=%T", err)
		walk.MsgBox(a.mw, "Export Failed", displayUntrustedText(err.Error()), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}

	diagnostics.Logf("report exported: groups=%d files=%d bytes=%d", document.GroupCount, document.FileCount, len(data))
	_ = a.statusLabel.SetText(fmt.Sprintf("Exported %d duplicate group(s) covering %d file(s) to %s.", document.GroupCount, document.FileCount, displayFilesystemPath(path)))
}
