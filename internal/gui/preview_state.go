package gui

// previewToken identifies one immutable preview request. Preview state is
// owned by the UI thread; workers only return the token they were given.
type previewToken struct {
	generation     uint64
	modelRevision  uint64
	folderRevision uint64
}

type previewGenerationState struct {
	nextGeneration uint64
	modelRevision  uint64
	active         previewToken
	hasActive      bool
}

func (state *previewGenerationState) begin(folderRevision uint64) previewToken {
	state.nextGeneration++
	token := previewToken{
		generation:     state.nextGeneration,
		modelRevision:  state.modelRevision,
		folderRevision: folderRevision,
	}
	state.active = token
	state.hasActive = true
	return token
}

// invalidate advances both the request generation and model revision. Results
// prepared from any earlier row snapshot are thereby rejected even if a caller
// accidentally retains and reposts an old token.
func (state *previewGenerationState) invalidate() {
	state.nextGeneration++
	state.modelRevision++
	state.active = previewToken{}
	state.hasActive = false
}

func (state *previewGenerationState) accepts(token previewToken, folderRevision uint64) bool {
	return state.hasActive &&
		state.active == token &&
		token.modelRevision == state.modelRevision &&
		token.folderRevision == folderRevision
}

// latestValueQueue keeps at most one pending value. One value may be actively
// processed by a consumer while this queue retains only the newest replacement.
// submit is called by the UI thread, so no producer-side mutex is necessary.
type latestValueQueue[T any] struct {
	pending chan T
}

func newLatestValueQueue[T any]() *latestValueQueue[T] {
	return &latestValueQueue[T]{pending: make(chan T, 1)}
}

func (queue *latestValueQueue[T]) submit(value T) {
	select {
	case queue.pending <- value:
		return
	default:
	}

	select {
	case <-queue.pending:
	default:
	}

	select {
	case queue.pending <- value:
	default:
		// The single producer is the UI thread. Reaching this branch means the
		// consumer accepted another value between drain and send, so there is
		// already no older pending work to replace.
	}
}

func (queue *latestValueQueue[T]) values() <-chan T {
	return queue.pending
}
