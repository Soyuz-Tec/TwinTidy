package gui

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var operationTestStart = time.Date(2026, time.July, 11, 12, 30, 0, 0, time.UTC)

func TestNewOperationState(t *testing.T) {
	state := newOperationState()

	if state.phase != phaseNoFolder {
		t.Fatalf("phase = %s, want %s", state.phase, phaseNoFolder)
	}
	if state.folder != "" || state.active != nil || state.cancel != nil {
		t.Fatalf("new state contains unexpected work: %+v", state)
	}
	if state.closeRequested || state.allowClose {
		t.Fatalf("new state unexpectedly permits or requests close: %+v", state)
	}
}

func TestAppPhaseStringCoversEveryPhase(t *testing.T) {
	phases := []appPhase{
		phaseNoFolder,
		phaseFolderReady,
		phaseSurfaceScanning,
		phaseSurfaceCancelling,
		phaseSurfaceReady,
		phaseDuplicateScanning,
		phaseDuplicateCancelling,
		phaseResultsReady,
		phaseExporting,
		phaseExportCancelling,
		phaseDeleting,
		phaseClosingAfterExport,
		phaseClosingAfterDelete,
		phaseClosing,
	}
	seen := make(map[string]appPhase, len(phases))
	for _, phase := range phases {
		name := phase.String()
		if name == "" {
			t.Fatalf("phase %d has an empty name", phase)
		}
		if previous, exists := seen[name]; exists {
			t.Fatalf("phases %d and %d share name %q", previous, phase, name)
		}
		seen[name] = phase
	}
	if name := appPhase(255).String(); !strings.Contains(name, "unknown phase") {
		t.Fatalf("unknown phase name = %q", name)
	}
}

func TestOperationKindStringCoversEveryKind(t *testing.T) {
	tests := []struct {
		kind operationKind
		want string
	}{
		{operationSurfaceScan, "surface scan"},
		{operationDuplicateScan, "duplicate scan"},
		{operationExport, "report export"},
		{operationDelete, "delete"},
	}
	for _, test := range tests {
		if got := test.kind.String(); got != test.want {
			t.Fatalf("kind %d String() = %q, want %q", test.kind, got, test.want)
		}
	}
	if name := operationKind(255).String(); !strings.Contains(name, "unknown operation") {
		t.Fatalf("unknown operation name = %q", name)
	}
}

func TestTransitionErrorIncludesEventAndPhase(t *testing.T) {
	state := newOperationState()
	_, err := state.beginDelete(operationTestStart)
	if !errors.Is(err, errInvalidOperationTransition) {
		t.Fatalf("error = %v, want transition error", err)
	}
	if message := err.Error(); !strings.Contains(message, "start delete") || !strings.Contains(message, phaseNoFolder.String()) {
		t.Fatalf("transition error lacks context: %q", message)
	}
}

func TestSelectFolderEstablishesNewFolderBoundary(t *testing.T) {
	state := newOperationState()
	initialGeneration := state.nextGeneration
	initialRevision := state.folderRevision

	if err := state.selectFolder(`C:\Data\A`); err != nil {
		t.Fatalf("selectFolder returned error: %v", err)
	}
	if state.phase != phaseFolderReady || state.folder != `C:\Data\A` {
		t.Fatalf("state after selection = %+v", state)
	}
	if state.folderRevision != initialRevision+1 {
		t.Fatalf("folder revision = %d, want %d", state.folderRevision, initialRevision+1)
	}
	if state.nextGeneration <= initialGeneration {
		t.Fatalf("generation did not advance: %d <= %d", state.nextGeneration, initialGeneration)
	}

	oldRevision := state.folderRevision
	oldGeneration := state.nextGeneration
	if err := state.selectFolder(`D:\Data\B`); err != nil {
		t.Fatalf("replace folder returned error: %v", err)
	}
	if state.folder != `D:\Data\B` || state.folderRevision != oldRevision+1 || state.nextGeneration <= oldGeneration {
		t.Fatalf("replacement did not establish a fresh boundary: %+v", state)
	}
}

func TestSelectFolderRejectsEmptyPathWithoutMutation(t *testing.T) {
	state := newOperationState()
	beforePhase := state.phase
	beforeFolder := state.folder
	beforeFolderRevision := state.folderRevision
	beforeGeneration := state.nextGeneration

	if err := state.selectFolder("  "); err == nil {
		t.Fatal("selectFolder accepted an empty path")
	}
	if state.phase != beforePhase || state.folder != beforeFolder || state.folderRevision != beforeFolderRevision || state.nextGeneration != beforeGeneration || state.active != nil || state.cancel != nil {
		t.Fatalf("state changed after invalid folder: %+v", state)
	}
}

func TestFolderAndResetPermissionsByPhase(t *testing.T) {
	tests := []struct {
		phase   appPhase
		allowed bool
	}{
		{phaseNoFolder, true},
		{phaseFolderReady, true},
		{phaseSurfaceScanning, false},
		{phaseSurfaceCancelling, false},
		{phaseSurfaceReady, true},
		{phaseDuplicateScanning, false},
		{phaseDuplicateCancelling, false},
		{phaseResultsReady, true},
		{phaseExporting, false},
		{phaseExportCancelling, false},
		{phaseDeleting, false},
		{phaseClosingAfterExport, false},
		{phaseClosingAfterDelete, false},
		{phaseClosing, false},
	}

	for _, test := range tests {
		t.Run(test.phase.String(), func(t *testing.T) {
			state := newOperationState()
			state.phase = test.phase
			if got := state.canChangeFolder(); got != test.allowed {
				t.Fatalf("canChangeFolder() = %v, want %v", got, test.allowed)
			}
			if got := state.canReset(); got != test.allowed {
				t.Fatalf("canReset() = %v, want %v", got, test.allowed)
			}

			selectErr := state.selectFolder(`C:\Replacement`)
			if test.allowed && selectErr != nil {
				t.Fatalf("selectFolder returned error: %v", selectErr)
			}
			if !test.allowed && !errors.Is(selectErr, errInvalidOperationTransition) {
				t.Fatalf("selectFolder error = %v, want transition error", selectErr)
			}
		})
	}
}

func TestSurfaceScanSuccessTransitionAndTokenSnapshot(t *testing.T) {
	state := stateWithFolder(t)
	cancelCalls := 0
	token, err := state.beginSurfaceScan(operationTestStart, func() { cancelCalls++ })
	if err != nil {
		t.Fatalf("beginSurfaceScan returned error: %v", err)
	}

	if state.phase != phaseSurfaceScanning || !state.accepts(token) {
		t.Fatalf("surface token was not activated: state=%+v token=%+v", state, token)
	}
	if token.folder != state.folder || token.folderRevision != state.folderRevision || token.startedAt != operationTestStart {
		t.Fatalf("surface token did not capture immutable inputs: %+v", token)
	}
	if !state.completeSurfaceScan(token, true) {
		t.Fatal("matching surface completion was rejected")
	}
	if state.phase != phaseSurfaceReady || state.active != nil || state.cancel != nil {
		t.Fatalf("surface success left invalid state: %+v", state)
	}
	if cancelCalls != 0 {
		t.Fatalf("successful scan invoked cancellation %d time(s)", cancelCalls)
	}
}

func TestSurfaceScanFailureReturnsToFolderReady(t *testing.T) {
	state := stateWithFolder(t)
	token, err := state.beginSurfaceScan(operationTestStart, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !state.completeSurfaceScan(token, false) {
		t.Fatal("matching failure was rejected")
	}
	if state.phase != phaseFolderReady {
		t.Fatalf("phase = %s, want %s", state.phase, phaseFolderReady)
	}
}

func TestSurfaceCancellationInvokesCancelOnceAndDiscardsSuccess(t *testing.T) {
	state := stateWithFolder(t)
	cancelCalls := 0
	token, err := state.beginSurfaceScan(operationTestStart, func() { cancelCalls++ })
	if err != nil {
		t.Fatal(err)
	}

	if !state.requestScanCancellation() {
		t.Fatal("first cancellation request was rejected")
	}
	if state.phase != phaseSurfaceCancelling || cancelCalls != 1 {
		t.Fatalf("cancellation state = %+v, calls = %d", state, cancelCalls)
	}
	if state.requestScanCancellation() {
		t.Fatal("repeated cancellation request reported a new transition")
	}
	if cancelCalls != 1 {
		t.Fatalf("cancel invoked %d times, want 1", cancelCalls)
	}
	if !state.accepts(token) {
		t.Fatal("cancelling state must still accept worker acknowledgement")
	}
	if !state.completeSurfaceScan(token, true) {
		t.Fatal("cancellation acknowledgement was rejected")
	}
	if state.phase != phaseFolderReady {
		t.Fatalf("cancelled success published a surface report: phase=%s", state.phase)
	}
}

func TestDuplicateScanSuccessFailureAndCancellation(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		state := stateWithSurface(t)
		token, err := state.beginDuplicateScan(operationTestStart, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !state.completeDuplicateScan(token, true) || state.phase != phaseResultsReady {
			t.Fatalf("duplicate success state = %+v", state)
		}
	})

	t.Run("failure", func(t *testing.T) {
		state := stateWithSurface(t)
		token, err := state.beginDuplicateScan(operationTestStart, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !state.completeDuplicateScan(token, false) || state.phase != phaseSurfaceReady {
			t.Fatalf("duplicate failure state = %+v", state)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		state := stateWithSurface(t)
		cancelCalls := 0
		token, err := state.beginDuplicateScan(operationTestStart, func() { cancelCalls++ })
		if err != nil {
			t.Fatal(err)
		}
		if !state.requestScanCancellation() || state.phase != phaseDuplicateCancelling {
			t.Fatalf("duplicate cancellation state = %+v", state)
		}
		if cancelCalls != 1 {
			t.Fatalf("cancel invoked %d times, want 1", cancelCalls)
		}
		if !state.completeDuplicateScan(token, true) || state.phase != phaseSurfaceReady {
			t.Fatalf("cancelled duplicate success was published: %+v", state)
		}
	})
}

func TestScanStartsRejectInvalidPhasesAndExistingWork(t *testing.T) {
	for _, phase := range []appPhase{
		phaseNoFolder,
		phaseSurfaceReady,
		phaseResultsReady,
		phaseDeleting,
		phaseClosing,
	} {
		t.Run("surface from "+phase.String(), func(t *testing.T) {
			state := newOperationState()
			state.phase = phase
			_, err := state.beginSurfaceScan(operationTestStart, nil)
			if !errors.Is(err, errInvalidOperationTransition) {
				t.Fatalf("error = %v, want transition error", err)
			}
		})
	}

	for _, phase := range []appPhase{
		phaseNoFolder,
		phaseFolderReady,
		phaseSurfaceScanning,
		phaseResultsReady,
		phaseDeleting,
		phaseClosing,
	} {
		t.Run("duplicate from "+phase.String(), func(t *testing.T) {
			state := newOperationState()
			state.phase = phase
			_, err := state.beginDuplicateScan(operationTestStart, nil)
			if !errors.Is(err, errInvalidOperationTransition) {
				t.Fatalf("error = %v, want transition error", err)
			}
		})
	}
}

func TestClearResultsTransition(t *testing.T) {
	state := stateWithResults(t)
	if err := state.clearResults(); err != nil {
		t.Fatalf("clearResults returned error: %v", err)
	}
	if state.phase != phaseSurfaceReady {
		t.Fatalf("phase = %s, want %s", state.phase, phaseSurfaceReady)
	}
	if err := state.clearResults(); !errors.Is(err, errInvalidOperationTransition) {
		t.Fatalf("second clear error = %v, want transition error", err)
	}
}

func TestResetCreatesBoundaryAndRejectsOldCompletion(t *testing.T) {
	state := stateWithSurface(t)
	oldRevision := state.folderRevision
	oldGeneration := state.nextGeneration

	if err := state.reset(); err != nil {
		t.Fatalf("reset returned error: %v", err)
	}
	if state.phase != phaseNoFolder || state.folder != "" {
		t.Fatalf("reset state = %+v", state)
	}
	if state.folderRevision != oldRevision+1 || state.nextGeneration <= oldGeneration {
		t.Fatalf("reset did not invalidate prior work: %+v", state)
	}
}

func TestResetRejectsBusyPhaseWithoutInvokingCancellation(t *testing.T) {
	state := stateWithFolder(t)
	cancelCalls := 0
	_, err := state.beginSurfaceScan(operationTestStart, func() { cancelCalls++ })
	if err != nil {
		t.Fatal(err)
	}

	if err := state.reset(); !errors.Is(err, errInvalidOperationTransition) {
		t.Fatalf("reset error = %v, want transition error", err)
	}
	if state.phase != phaseSurfaceScanning || cancelCalls != 0 {
		t.Fatalf("rejected reset mutated work: calls=%d state=%+v", cancelCalls, state)
	}
}

func TestStaleAndMismatchedTokensAreRejected(t *testing.T) {
	state := stateWithFolder(t)
	token, err := state.beginSurfaceScan(operationTestStart, nil)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func(operationToken) operationToken
	}{
		{"generation", func(value operationToken) operationToken { value.generation++; return value }},
		{"folder revision", func(value operationToken) operationToken { value.folderRevision++; return value }},
		{"folder", func(value operationToken) operationToken { value.folder += `\other`; return value }},
		{"kind", func(value operationToken) operationToken { value.kind = operationDelete; return value }},
		{"start time", func(value operationToken) operationToken {
			value.startedAt = value.startedAt.Add(time.Second)
			return value
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			beforePhase := state.phase
			if state.completeSurfaceScan(test.mutate(token), true) {
				t.Fatal("mismatched token was accepted")
			}
			if state.phase != beforePhase || !state.accepts(token) {
				t.Fatalf("rejected token mutated active state: %+v", state)
			}
		})
	}

	if !state.completeSurfaceScan(token, true) {
		t.Fatal("matching token was rejected")
	}
	if state.completeSurfaceScan(token, true) {
		t.Fatal("completed token was accepted twice")
	}
}

func TestAcceptsRejectsUnknownOperationKind(t *testing.T) {
	state := stateWithFolder(t)
	token := operationToken{
		generation:     state.nextGeneration + 1,
		folderRevision: state.folderRevision,
		kind:           operationKind(255),
		folder:         state.folder,
		startedAt:      operationTestStart,
	}
	state.active = &token
	state.phase = phaseSurfaceScanning

	if state.accepts(token) {
		t.Fatal("unknown operation kind was accepted")
	}
}

func TestFolderChangeRejectsOldSurfaceCompletion(t *testing.T) {
	state := stateWithFolder(t)
	token, err := state.beginSurfaceScan(operationTestStart, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !state.requestScanCancellation() || !state.completeSurfaceScan(token, false) {
		t.Fatal("failed to acknowledge surface cancellation")
	}
	if err := state.selectFolder(`D:\NewFolder`); err != nil {
		t.Fatal(err)
	}

	if state.completeSurfaceScan(token, true) {
		t.Fatal("old folder completion was accepted")
	}
	if state.phase != phaseFolderReady || state.folder != `D:\NewFolder` {
		t.Fatalf("old callback mutated new folder state: %+v", state)
	}
}

func TestDeleteCompletionAndWrongGeneration(t *testing.T) {
	state := stateWithResults(t)
	token, err := state.beginDelete(operationTestStart)
	if err != nil {
		t.Fatal(err)
	}
	if state.phase != phaseDeleting || !state.accepts(token) {
		t.Fatalf("delete token was not activated: %+v", state)
	}

	wrong := token
	wrong.generation++
	if accepted, shouldClose := state.completeDelete(wrong); accepted || shouldClose {
		t.Fatalf("wrong generation returned accepted=%v close=%v", accepted, shouldClose)
	}
	if state.phase != phaseDeleting {
		t.Fatalf("wrong delete callback changed phase to %s", state.phase)
	}

	if accepted, shouldClose := state.completeDelete(token); !accepted || shouldClose {
		t.Fatalf("matching delete returned accepted=%v close=%v", accepted, shouldClose)
	}
	if state.phase != phaseResultsReady || state.active != nil {
		t.Fatalf("delete completion state = %+v", state)
	}
}

func TestExportStartCancelAndCompletion(t *testing.T) {
	state := stateWithResults(t)
	cancelCalls := 0
	token, err := state.beginExport(operationTestStart, func() { cancelCalls++ })
	if err != nil {
		t.Fatal(err)
	}
	if state.phase != phaseExporting || !state.accepts(token) {
		t.Fatalf("export token was not activated: %+v", state)
	}
	if !state.requestExportCancellation() || state.phase != phaseExportCancelling || cancelCalls != 1 {
		t.Fatalf("export cancellation state=%+v calls=%d", state, cancelCalls)
	}
	if state.requestExportCancellation() || cancelCalls != 1 {
		t.Fatalf("repeated cancellation changed state: %+v calls=%d", state, cancelCalls)
	}

	wrong := token
	wrong.generation++
	if accepted, shouldClose := state.completeExport(wrong); accepted || shouldClose {
		t.Fatalf("stale completion returned accepted=%v close=%v", accepted, shouldClose)
	}
	if accepted, shouldClose := state.completeExport(token); !accepted || shouldClose {
		t.Fatalf("matching completion returned accepted=%v close=%v", accepted, shouldClose)
	}
	if state.phase != phaseResultsReady || state.active != nil || state.cancel != nil {
		t.Fatalf("completed export state = %+v", state)
	}
}

func TestExportStartRequiresResults(t *testing.T) {
	for _, phase := range []appPhase{phaseNoFolder, phaseFolderReady, phaseSurfaceReady, phaseDuplicateScanning, phaseExporting, phaseDeleting, phaseClosing} {
		t.Run(phase.String(), func(t *testing.T) {
			state := newOperationState()
			state.phase = phase
			_, err := state.beginExport(operationTestStart, nil)
			if !errors.Is(err, errInvalidOperationTransition) {
				t.Fatalf("error = %v, want transition error", err)
			}
		})
	}
}

func TestCloseDuringExportCancelsAndDefersUntilCleanup(t *testing.T) {
	state := stateWithResults(t)
	cancelCalls := 0
	token, err := state.beginExport(operationTestStart, func() { cancelCalls++ })
	if err != nil {
		t.Fatal(err)
	}

	if disposition := state.requestClose(); disposition != closeDeferred {
		t.Fatalf("requestClose() = %v, want %v", disposition, closeDeferred)
	}
	if state.phase != phaseClosingAfterExport || cancelCalls != 1 || !state.accepts(token) {
		t.Fatalf("deferred export close state=%+v calls=%d", state, cancelCalls)
	}
	if disposition := state.requestClose(); disposition != closeDeferred || cancelCalls != 1 {
		t.Fatalf("repeated close disposition=%v calls=%d", disposition, cancelCalls)
	}

	wrong := token
	wrong.folderRevision++
	if accepted, shouldClose := state.completeExport(wrong); accepted || shouldClose {
		t.Fatal("stale export completion released deferred close")
	}
	if accepted, shouldClose := state.completeExport(token); !accepted || !shouldClose {
		t.Fatalf("completion returned accepted=%v close=%v", accepted, shouldClose)
	}
	if state.phase != phaseClosing || !state.allowClose || state.active != nil {
		t.Fatalf("completed deferred close state = %+v", state)
	}
}

func TestDeleteStartRequiresResults(t *testing.T) {
	for _, phase := range []appPhase{phaseNoFolder, phaseFolderReady, phaseSurfaceReady, phaseDuplicateScanning, phaseDeleting, phaseClosing} {
		t.Run(phase.String(), func(t *testing.T) {
			state := newOperationState()
			state.phase = phase
			_, err := state.beginDelete(operationTestStart)
			if !errors.Is(err, errInvalidOperationTransition) {
				t.Fatalf("error = %v, want transition error", err)
			}
		})
	}
}

func TestCloseDuringDeleteDefersUntilMatchingCompletion(t *testing.T) {
	state := stateWithResults(t)
	token, err := state.beginDelete(operationTestStart)
	if err != nil {
		t.Fatal(err)
	}

	if disposition := state.requestClose(); disposition != closeDeferred {
		t.Fatalf("requestClose() = %v, want %v", disposition, closeDeferred)
	}
	if state.phase != phaseClosingAfterDelete || !state.closeRequested || state.allowClose {
		t.Fatalf("deferred-close state = %+v", state)
	}
	if !state.accepts(token) {
		t.Fatal("deferred close invalidated the destructive operation")
	}
	if disposition := state.requestClose(); disposition != closeDeferred {
		t.Fatalf("repeated requestClose() = %v, want %v", disposition, closeDeferred)
	}

	wrong := token
	wrong.folderRevision++
	if accepted, shouldClose := state.completeDelete(wrong); accepted || shouldClose {
		t.Fatal("stale delete completion released deferred close")
	}
	if !state.accepts(token) || state.allowClose {
		t.Fatal("stale completion corrupted deferred-delete state")
	}

	if accepted, shouldClose := state.completeDelete(token); !accepted || !shouldClose {
		t.Fatalf("completion returned accepted=%v close=%v", accepted, shouldClose)
	}
	if state.phase != phaseClosing || !state.allowClose || state.active != nil {
		t.Fatalf("completed deferred-close state = %+v", state)
	}
	if disposition := state.requestClose(); disposition != closeNow {
		t.Fatalf("final requestClose() = %v, want %v", disposition, closeNow)
	}
}

func TestCloseDuringScanCancelsInvalidatesAndRejectsCompletion(t *testing.T) {
	state := stateWithSurface(t)
	cancelCalls := 0
	token, err := state.beginDuplicateScan(operationTestStart, func() { cancelCalls++ })
	if err != nil {
		t.Fatal(err)
	}

	if disposition := state.requestClose(); disposition != closeNow {
		t.Fatalf("requestClose() = %v, want %v", disposition, closeNow)
	}
	if state.phase != phaseClosing || !state.closeRequested || !state.allowClose {
		t.Fatalf("closing state = %+v", state)
	}
	if cancelCalls != 1 || state.active != nil || state.cancel != nil {
		t.Fatalf("close cancellation bookkeeping: calls=%d state=%+v", cancelCalls, state)
	}
	if state.completeDuplicateScan(token, true) {
		t.Fatal("completion after close was accepted")
	}
	if disposition := state.requestClose(); disposition != closeNow || cancelCalls != 1 {
		t.Fatalf("repeated close disposition=%v cancel calls=%d", disposition, cancelCalls)
	}
}

func TestCloseDispositionForEveryNonDeletePhase(t *testing.T) {
	phases := []appPhase{
		phaseNoFolder,
		phaseFolderReady,
		phaseSurfaceScanning,
		phaseSurfaceCancelling,
		phaseSurfaceReady,
		phaseDuplicateScanning,
		phaseDuplicateCancelling,
		phaseResultsReady,
		phaseClosing,
	}

	for _, phase := range phases {
		t.Run(phase.String(), func(t *testing.T) {
			state := newOperationState()
			state.phase = phase
			if disposition := state.requestClose(); disposition != closeNow {
				t.Fatalf("requestClose() = %v, want %v", disposition, closeNow)
			}
			if state.phase != phaseClosing || !state.allowClose || !state.closeRequested {
				t.Fatalf("close state = %+v", state)
			}
		})
	}
}

func TestInvalidateActiveOperationCancelsOnceAndAdvancesGeneration(t *testing.T) {
	state := stateWithFolder(t)
	cancelCalls := 0
	token, err := state.beginSurfaceScan(operationTestStart, func() { cancelCalls++ })
	if err != nil {
		t.Fatal(err)
	}
	previousGeneration := state.nextGeneration

	state.invalidateActiveOperation()
	if cancelCalls != 1 || state.active != nil || state.cancel != nil {
		t.Fatalf("invalidation bookkeeping: calls=%d state=%+v", cancelCalls, state)
	}
	if state.nextGeneration <= previousGeneration {
		t.Fatalf("generation = %d, want greater than %d", state.nextGeneration, previousGeneration)
	}
	if state.accepts(token) {
		t.Fatal("invalidated token remains acceptable")
	}

	state.invalidateActiveOperation()
	if cancelCalls != 1 {
		t.Fatalf("repeated invalidation invoked cancel %d times", cancelCalls)
	}
}

func stateWithFolder(t *testing.T) operationState {
	t.Helper()
	state := newOperationState()
	if err := state.selectFolder(`C:\Data\TwinTidy`); err != nil {
		t.Fatalf("selectFolder returned error: %v", err)
	}
	return state
}

func stateWithSurface(t *testing.T) operationState {
	t.Helper()
	state := stateWithFolder(t)
	token, err := state.beginSurfaceScan(operationTestStart, nil)
	if err != nil {
		t.Fatalf("beginSurfaceScan returned error: %v", err)
	}
	if !state.completeSurfaceScan(token, true) {
		t.Fatal("completeSurfaceScan rejected matching token")
	}
	return state
}

func stateWithResults(t *testing.T) operationState {
	t.Helper()
	state := stateWithSurface(t)
	token, err := state.beginDuplicateScan(operationTestStart, nil)
	if err != nil {
		t.Fatalf("beginDuplicateScan returned error: %v", err)
	}
	if !state.completeDuplicateScan(token, true) {
		t.Fatal("completeDuplicateScan rejected matching token")
	}
	return state
}
