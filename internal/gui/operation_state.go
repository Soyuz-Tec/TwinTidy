package gui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// appPhase is the single source of truth for which foreground operation the
// desktop application may start and which asynchronous completion it may
// accept. operationState is owned by the UI thread; background goroutines must
// communicate with it only by posting an operationToken back to that thread.
type appPhase uint8

const (
	phaseNoFolder appPhase = iota
	phaseFolderReady
	phaseSurfaceScanning
	phaseSurfaceCancelling
	phaseSurfaceReady
	phaseDuplicateScanning
	phaseDuplicateCancelling
	phaseResultsReady
	phaseDeleting
	phaseClosingAfterDelete
	phaseClosing
)

func (phase appPhase) String() string {
	switch phase {
	case phaseNoFolder:
		return "no folder"
	case phaseFolderReady:
		return "folder ready"
	case phaseSurfaceScanning:
		return "surface scanning"
	case phaseSurfaceCancelling:
		return "surface scan cancelling"
	case phaseSurfaceReady:
		return "surface ready"
	case phaseDuplicateScanning:
		return "duplicate scanning"
	case phaseDuplicateCancelling:
		return "duplicate scan cancelling"
	case phaseResultsReady:
		return "results ready"
	case phaseDeleting:
		return "deleting"
	case phaseClosingAfterDelete:
		return "closing after delete"
	case phaseClosing:
		return "closing"
	default:
		return fmt.Sprintf("unknown phase %d", uint8(phase))
	}
}

type operationKind uint8

const (
	operationSurfaceScan operationKind = iota + 1
	operationDuplicateScan
	operationDelete
)

func (kind operationKind) String() string {
	switch kind {
	case operationSurfaceScan:
		return "surface scan"
	case operationDuplicateScan:
		return "duplicate scan"
	case operationDelete:
		return "delete"
	default:
		return fmt.Sprintf("unknown operation %d", uint8(kind))
	}
}

// operationToken identifies one immutable unit of asynchronous work. Folder
// revision prevents work from one folder being published under another even if
// a future refactor accidentally reuses an operation generation.
type operationToken struct {
	generation     uint64
	folderRevision uint64
	kind           operationKind
	folder         string
	startedAt      time.Time
}

type closeDisposition uint8

const (
	closeNow closeDisposition = iota
	closeDeferred
)

var errInvalidOperationTransition = errors.New("invalid operation transition")

type operationTransitionError struct {
	event string
	phase appPhase
}

func (err *operationTransitionError) Error() string {
	return fmt.Sprintf("%s is not allowed while application is %s", err.event, err.phase)
}

func (err *operationTransitionError) Unwrap() error {
	return errInvalidOperationTransition
}

// operationState is intentionally independent of Walk so its safety rules can
// be exhaustively unit tested without a Windows message loop.
type operationState struct {
	phase          appPhase
	folder         string
	folderRevision uint64
	nextGeneration uint64

	active *operationToken
	cancel context.CancelFunc

	closeRequested bool
	allowClose     bool
}

func newOperationState() operationState {
	return operationState{phase: phaseNoFolder}
}

func (state *operationState) selectFolder(folder string) error {
	if strings.TrimSpace(folder) == "" {
		return errors.New("folder is required")
	}
	if !state.canChangeFolder() {
		return state.transitionError("select folder")
	}

	state.invalidateActiveOperation()
	state.folderRevision++
	state.folder = folder
	state.phase = phaseFolderReady
	return nil
}

func (state *operationState) reset() error {
	if !state.canReset() {
		return state.transitionError("reset")
	}

	state.invalidateActiveOperation()
	state.folderRevision++
	state.folder = ""
	state.phase = phaseNoFolder
	return nil
}

func (state *operationState) clearResults() error {
	if state.phase != phaseResultsReady {
		return state.transitionError("clear results")
	}
	state.phase = phaseSurfaceReady
	return nil
}

func (state *operationState) beginSurfaceScan(startedAt time.Time, cancel context.CancelFunc) (operationToken, error) {
	if state.phase != phaseFolderReady {
		return operationToken{}, state.transitionError("start surface scan")
	}
	return state.beginOperation(operationSurfaceScan, phaseSurfaceScanning, startedAt, cancel), nil
}

// completeSurfaceScan returns false for stale or mismatched callbacks. A
// completion received after cancellation never publishes a successful report.
func (state *operationState) completeSurfaceScan(token operationToken, successful bool) bool {
	if token.kind != operationSurfaceScan || !state.accepts(token) {
		return false
	}

	wasCancelling := state.phase == phaseSurfaceCancelling
	state.clearActiveOperation()
	if successful && !wasCancelling {
		state.phase = phaseSurfaceReady
	} else {
		state.phase = phaseFolderReady
	}
	return true
}

func (state *operationState) beginDuplicateScan(startedAt time.Time, cancel context.CancelFunc) (operationToken, error) {
	if state.phase != phaseSurfaceReady {
		return operationToken{}, state.transitionError("start duplicate scan")
	}
	return state.beginOperation(operationDuplicateScan, phaseDuplicateScanning, startedAt, cancel), nil
}

// completeDuplicateScan returns false for stale or mismatched callbacks. A
// cancelled or failed duplicate scan preserves the valid surface snapshot.
func (state *operationState) completeDuplicateScan(token operationToken, successful bool) bool {
	if token.kind != operationDuplicateScan || !state.accepts(token) {
		return false
	}

	wasCancelling := state.phase == phaseDuplicateCancelling
	state.clearActiveOperation()
	if successful && !wasCancelling {
		state.phase = phaseResultsReady
	} else {
		state.phase = phaseSurfaceReady
	}
	return true
}

// requestScanCancellation is idempotent. It invokes the stored cancellation
// function at most once while retaining the token so the matching worker can
// acknowledge cancellation and drive the final transition.
func (state *operationState) requestScanCancellation() bool {
	switch state.phase {
	case phaseSurfaceScanning:
		state.phase = phaseSurfaceCancelling
	case phaseDuplicateScanning:
		state.phase = phaseDuplicateCancelling
	default:
		return false
	}

	state.invokeCancellation()
	return true
}

func (state *operationState) beginDelete(startedAt time.Time) (operationToken, error) {
	if state.phase != phaseResultsReady {
		return operationToken{}, state.transitionError("start delete")
	}
	return state.beginOperation(operationDelete, phaseDeleting, startedAt, nil), nil
}

// completeDelete returns whether the completion was accepted and whether a
// previously deferred window close should now be retried. Delete failure does
// not change this transition: the UI applies the result and remains reviewable.
func (state *operationState) completeDelete(token operationToken) (accepted bool, shouldClose bool) {
	if token.kind != operationDelete || !state.accepts(token) {
		return false, false
	}

	closing := state.phase == phaseClosingAfterDelete
	state.clearActiveOperation()
	if closing {
		state.phase = phaseClosing
		state.allowClose = true
		return true, true
	}

	state.phase = phaseResultsReady
	return true, false
}

// requestClose allows normal shutdown immediately, but a destructive operation
// is never abandoned midway. Closing during delete is vetoed until the matching
// delete completion has been applied.
func (state *operationState) requestClose() closeDisposition {
	if state.allowClose {
		return closeNow
	}

	state.closeRequested = true
	switch state.phase {
	case phaseDeleting:
		state.phase = phaseClosingAfterDelete
		return closeDeferred
	case phaseClosingAfterDelete:
		return closeDeferred
	default:
		state.invalidateActiveOperation()
		state.phase = phaseClosing
		state.allowClose = true
		return closeNow
	}
}

func (state *operationState) canChangeFolder() bool {
	switch state.phase {
	case phaseNoFolder, phaseFolderReady, phaseSurfaceReady, phaseResultsReady:
		return true
	default:
		return false
	}
}

func (state *operationState) canReset() bool {
	return state.canChangeFolder()
}

// accepts must be called on the UI thread inside the synchronized callback,
// not merely before posting it to Walk's synchronization queue.
func (state *operationState) accepts(token operationToken) bool {
	if state.active == nil || *state.active != token {
		return false
	}

	switch token.kind {
	case operationSurfaceScan:
		return state.phase == phaseSurfaceScanning || state.phase == phaseSurfaceCancelling
	case operationDuplicateScan:
		return state.phase == phaseDuplicateScanning || state.phase == phaseDuplicateCancelling
	case operationDelete:
		return state.phase == phaseDeleting || state.phase == phaseClosingAfterDelete
	default:
		return false
	}
}

func (state *operationState) beginOperation(kind operationKind, phase appPhase, startedAt time.Time, cancel context.CancelFunc) operationToken {
	state.nextGeneration++
	token := operationToken{
		generation:     state.nextGeneration,
		folderRevision: state.folderRevision,
		kind:           kind,
		folder:         state.folder,
		startedAt:      startedAt,
	}
	state.active = &token
	state.cancel = cancel
	state.phase = phase
	return token
}

// invalidateActiveOperation makes every previously issued token stale. It is
// used for folder/reset/close boundaries where no worker completion should be
// allowed to mutate the next UI state.
func (state *operationState) invalidateActiveOperation() {
	state.invokeCancellation()
	state.active = nil
	state.nextGeneration++
}

func (state *operationState) clearActiveOperation() {
	state.active = nil
	state.cancel = nil
}

func (state *operationState) invokeCancellation() {
	cancel := state.cancel
	state.cancel = nil
	if cancel != nil {
		cancel()
	}
}

func (state *operationState) transitionError(event string) error {
	return &operationTransitionError{event: event, phase: state.phase}
}
