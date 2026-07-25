//go:build windows

package gui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Soyuz-Tec/twintidy/internal/diagnostics"
	"github.com/Soyuz-Tec/twintidy/internal/scanner"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

// Hardlink consolidation (ADR 0009) reuses the recycle planning types: the
// same keeper invariant applies, and a consolidation plan is a recycle plan
// whose action is "replace with a hardlink" instead of "move to the bin".

type consolidateGroupResult struct {
	groupNumber int
	result      scanner.HardlinkResult
}

type consolidateBatchResult struct {
	groups        []consolidateGroupResult
	internalError string
}

type consolidateOutcome struct {
	consolidated  int
	changed       int
	protected     int
	crossVolume   int
	cancelled     int
	failed        int
	requestErrors []string
	backupPaths   []string
	internalError string
}

// consolidateConfirmationMessage lists every link target and keeper, and
// names the one observable behavior change up front, as ADR 0009 requires:
// consolidated paths become one shared file.
func consolidateConfirmationMessage(plan recyclePlan) (string, error) {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Consolidate exactly %d checked duplicate file(s) into hardlinks of their kept copy?\r\n\r\n", plan.selectedCount))
	builder.WriteString("IMPORTANT: after consolidation, each LINK path below and its KEEP copy become ONE shared file. Editing the file at any of those paths changes it at all of them. Deleting one path does not free space until every linked path is deleted.\r\n\r\n")
	builder.WriteString("No content is lost: every path keeps resolving to the same bytes, and TwinTidy revalidates file identity and content immediately before each step. Changed, protected, cross-volume, cancelled, or failed files remain in place unchanged.\r\n\r\n")

	entryCount := 0
	for _, planned := range plan.groups {
		builder.WriteString(fmt.Sprintf("Group %d\r\n", planned.groupNumber))
		selectedPaths := make(map[string]struct{}, len(planned.request.Selected))
		for _, selected := range planned.request.Selected {
			selectedPaths[strings.ToLower(filepath.Clean(selected.Path))] = struct{}{}
			builder.WriteString("  LINK: ")
			builder.WriteString(displayFilesystemPath(selected.Path))
			builder.WriteString("\r\n")
			entryCount++
		}
		for _, file := range planned.request.Group.Files {
			if _, selected := selectedPaths[strings.ToLower(filepath.Clean(file.Path))]; selected {
				continue
			}
			builder.WriteString("  KEEP: ")
			builder.WriteString(displayFilesystemPath(file.Path))
			builder.WriteString("\r\n")
			entryCount++
		}
		builder.WriteString("\r\n")
	}

	if entryCount > maxRecycleConfirmationEntries || builder.Len() > maxRecycleConfirmationChars {
		return "", errRecycleConfirmationTooLarge
	}
	return builder.String(), nil
}

func (a *windowsApp) confirmConsolidateSelected() {
	if !scanner.HardlinkConsolidationSupported() {
		walk.MsgBox(a.mw, "Consolidation Unavailable", "No files were changed. Hardlink consolidation requires the Windows identity-bound adapter.", walk.MsgBoxOK|walk.MsgBoxIconInformation)
		return
	}
	plan, err := buildRecyclePlan(a.model.rows)
	if err != nil {
		if errors.Is(err, errRecycleWouldRemoveAllCopies) {
			walk.MsgBox(a.mw, "Keep One Copy", "TwinTidy will not consolidate every physical copy in a duplicate group. Uncheck at least one verified keeper in each group.", walk.MsgBoxOK|walk.MsgBoxIconWarning)
			return
		}
		walk.MsgBox(a.mw, "Cannot Consolidate Selection", displayUntrustedText(err.Error()), walk.MsgBoxOK|walk.MsgBoxIconInformation)
		return
	}

	message, err := consolidateConfirmationMessage(plan)
	if err != nil {
		walk.MsgBox(a.mw, "Review A Smaller Batch", fmt.Sprintf("This selection contains %d checked target(s) and too many target/keeper paths for the native confirmation dialog. No files were changed.\r\n\r\nSelect a smaller batch so TwinTidy can show every link target and retained keeper before approval.", plan.selectedCount), walk.MsgBoxOK|walk.MsgBoxIconInformation)
		return
	}
	result := walk.MsgBox(a.mw, "Consolidate Checked Duplicates", message, walk.MsgBoxYesNo|walk.MsgBoxIconWarning)
	if result != win.IDYES {
		return
	}

	token, err := a.operation.beginDelete(time.Now())
	if err != nil {
		diagnostics.Logf("consolidate start rejected: phase=%s error_type=%T", a.operation.phase, err)
		return
	}
	a.setPreviewMessage("Revalidating checked duplicate files before consolidating them into hardlinks.")
	_ = a.statusLabel.SetText("Revalidating and consolidating checked duplicate files. This operation cannot be cancelled midway.")
	a.renderFromPhase()
	diagnostics.Logf("consolidate started: generation=%d groups=%d selected=%d", token.generation, len(plan.groups), plan.selectedCount)

	go func(plan recyclePlan, token operationToken) {
		batch := consolidateBatchResult{groups: make([]consolidateGroupResult, 0, len(plan.groups))}
		defer func() {
			fields := map[string]string{
				"generation":     fmt.Sprint(token.generation),
				"group_count":    fmt.Sprint(len(plan.groups)),
				"selected_count": fmt.Sprint(plan.selectedCount),
			}
			recovered := recover()
			if crashErr := diagnostics.PanicToError("consolidate workflow", recovered, fields); crashErr != nil {
				batch.internalError = crashErr.Error()
			}
			outcome := summarizeConsolidateBatch(batch)
			diagnostics.Logf("consolidate ended: generation=%d consolidated=%d changed=%d protected=%d cross_volume=%d cancelled=%d failed=%d backups=%d request_errors=%d internal_error=%t", token.generation, outcome.consolidated, outcome.changed, outcome.protected, outcome.crossVolume, outcome.cancelled, outcome.failed, len(outcome.backupPaths), len(outcome.requestErrors), outcome.internalError != "")
			a.synchronizeUI(func() {
				a.applyConsolidateBatch(token, batch)
			})
		}()

		ctx := context.Background()
		for _, planned := range plan.groups {
			batch.groups = append(batch.groups, consolidateGroupResult{
				groupNumber: planned.groupNumber,
				result:      scanner.ConsolidateExactDuplicates(ctx, planned.request),
			})
		}
	}(plan, token)
}

func summarizeConsolidateBatch(batch consolidateBatchResult) consolidateOutcome {
	outcome := consolidateOutcome{internalError: batch.internalError}
	for _, group := range batch.groups {
		if group.result.RequestError != "" {
			outcome.requestErrors = append(outcome.requestErrors, fmt.Sprintf("Group %d: %s", group.groupNumber, group.result.RequestError))
		}
		for _, item := range group.result.Items {
			if item.BackupPath != "" {
				outcome.backupPaths = append(outcome.backupPaths, item.BackupPath)
			}
			switch item.Status {
			case scanner.HardlinkStatusConsolidated:
				outcome.consolidated++
			case scanner.HardlinkStatusSkippedChanged:
				outcome.changed++
			case scanner.HardlinkStatusSkippedProtected:
				outcome.protected++
			case scanner.HardlinkStatusSkippedCrossVolume:
				outcome.crossVolume++
			case scanner.HardlinkStatusCancelled:
				outcome.cancelled++
			default:
				outcome.failed++
			}
		}
	}
	return outcome
}

func consolidateRowStatus(status scanner.HardlinkStatus) string {
	switch status {
	case scanner.HardlinkStatusConsolidated:
		return "Consolidated - shared copy"
	case scanner.HardlinkStatusSkippedChanged:
		return "Changed - kept"
	case scanner.HardlinkStatusSkippedProtected:
		return "Protected - kept"
	case scanner.HardlinkStatusSkippedCrossVolume:
		return "Different volume - kept"
	case scanner.HardlinkStatusCancelled:
		return "Cancelled - kept"
	default:
		return "Consolidation failed - kept"
	}
}

// applyConsolidateBatchToModel annotates rows rather than removing them:
// consolidated files still exist at their paths, now as shared copies. All
// checkboxes clear so a stale selection cannot feed a second operation.
func applyConsolidateBatchToModel(model *duplicateTableModel, batch consolidateBatchResult) consolidateOutcome {
	outcome := summarizeConsolidateBatch(batch)
	itemsByPath := make(map[string]scanner.HardlinkItemResult)
	for _, group := range batch.groups {
		for _, item := range group.result.Items {
			itemsByPath[strings.ToLower(filepath.Clean(item.Path))] = item
		}
	}
	for index := range model.rows {
		model.rows[index].Selected = false
		item, exists := itemsByPath[strings.ToLower(filepath.Clean(model.rows[index].File.Path))]
		if exists {
			model.rows[index].Status = consolidateRowStatus(item.Status)
		}
	}
	return outcome
}

func (a *windowsApp) applyConsolidateBatch(token operationToken, batch consolidateBatchResult) {
	if !a.operation.accepts(token) {
		diagnostics.Logf("stale consolidate completion ignored: generation=%d folder_revision=%d", token.generation, token.folderRevision)
		return
	}

	outcome := applyConsolidateBatchToModel(a.model, batch)
	accepted, shouldClose := a.operation.completeDelete(token)
	if !accepted {
		return
	}
	if a.table != nil {
		_ = a.table.SetSelectedIndexes(nil)
		_ = a.table.SetCurrentIndex(-1)
	}
	a.publishRows()
	a.progress.SetValue(1000)
	a.renderFromPhase()

	_ = a.statusLabel.SetText(consolidateOutcomeSummary(outcome))
	if shouldClose {
		a.mw.Close()
		return
	}
	if consolidateOutcomeHasIssues(outcome) {
		walk.MsgBox(a.mw, "Consolidation Results", consolidateIssueSummary(outcome), walk.MsgBoxOK|walk.MsgBoxIconWarning)
	}
}

func consolidateOutcomeSummary(outcome consolidateOutcome) string {
	parts := []string{fmt.Sprintf("Consolidated %d file(s) into shared hardlinked copies.", outcome.consolidated)}
	if retained := outcome.changed + outcome.protected + outcome.crossVolume + outcome.cancelled + outcome.failed; retained > 0 {
		parts = append(parts, fmt.Sprintf("%d file(s) were kept unchanged.", retained))
	}
	return strings.Join(parts, " ")
}

func consolidateOutcomeHasIssues(outcome consolidateOutcome) bool {
	return outcome.failed > 0 || len(outcome.requestErrors) > 0 || len(outcome.backupPaths) > 0 || outcome.internalError != ""
}

// consolidateIssueSummary surfaces every retained backup path verbatim: a
// reported backup contains the user's original bytes and must never be
// discovered by accident.
func consolidateIssueSummary(outcome consolidateOutcome) string {
	var builder strings.Builder
	builder.WriteString("Some files were not fully consolidated. No content was lost.\r\n")
	if outcome.failed > 0 {
		builder.WriteString(fmt.Sprintf("\r\n%d operation(s) failed and their files remain in place.\r\n", outcome.failed))
	}
	for _, requestError := range outcome.requestErrors {
		builder.WriteString("\r\n")
		builder.WriteString(displayUntrustedText(requestError))
		builder.WriteString("\r\n")
	}
	if len(outcome.backupPaths) > 0 {
		builder.WriteString("\r\nYour original content for the affected file(s) is preserved at:\r\n")
		for _, backup := range outcome.backupPaths {
			builder.WriteString("  ")
			builder.WriteString(displayFilesystemPath(backup))
			builder.WriteString("\r\n")
		}
		builder.WriteString("Review these files before removing them.\r\n")
	}
	if outcome.internalError != "" {
		builder.WriteString("\r\nInternal error: ")
		builder.WriteString(displayUntrustedText(outcome.internalError))
		builder.WriteString("\r\n")
	}
	return builder.String()
}
