package gui

import "testing"

func TestPreviewGenerationLatestRequestWins(t *testing.T) {
	state := previewGenerationState{}
	first := state.begin(7)
	second := state.begin(7)

	if state.accepts(first, 7) {
		t.Fatal("superseded preview token was accepted")
	}
	if !state.accepts(second, 7) {
		t.Fatal("latest preview token was rejected")
	}
	if second.generation <= first.generation {
		t.Fatalf("generation did not advance: first=%d second=%d", first.generation, second.generation)
	}
}

func TestPreviewInvalidationRejectsPreparedResult(t *testing.T) {
	state := previewGenerationState{}
	token := state.begin(4)
	originalRevision := token.modelRevision

	state.invalidate()
	if state.accepts(token, 4) {
		t.Fatal("invalidated preview token was accepted")
	}
	if state.modelRevision <= originalRevision {
		t.Fatalf("model revision did not advance: got %d, started %d", state.modelRevision, originalRevision)
	}
}

func TestPreviewFolderRevisionIsPartOfAcceptance(t *testing.T) {
	state := previewGenerationState{}
	token := state.begin(11)

	if state.accepts(token, 12) {
		t.Fatal("preview token from another folder revision was accepted")
	}
	if !state.accepts(token, 11) {
		t.Fatal("preview token for current folder revision was rejected")
	}
}

func TestLatestValueQueueKeepsOnlyNewestPendingValue(t *testing.T) {
	queue := newLatestValueQueue[int]()
	queue.submit(1)
	queue.submit(2)
	queue.submit(3)

	if got := <-queue.values(); got != 3 {
		t.Fatalf("pending value = %d, want latest value 3", got)
	}
	select {
	case unexpected := <-queue.values():
		t.Fatalf("queue retained extra value %d", unexpected)
	default:
	}
}

func TestLatestValueQueueAllowsOneActiveAndOnePendingValue(t *testing.T) {
	queue := newLatestValueQueue[int]()
	queue.submit(1)
	active := <-queue.values()
	if active != 1 {
		t.Fatalf("active value = %d, want 1", active)
	}

	queue.submit(2)
	queue.submit(3)
	if pending := <-queue.values(); pending != 3 {
		t.Fatalf("pending value = %d, want latest replacement 3", pending)
	}
}
