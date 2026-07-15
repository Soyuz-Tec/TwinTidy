# ADR 0007: Stream report exports through a cancellable atomic writer

- Status: Accepted
- Date: 2026-07-15

## Context

TwinTidy can review hundreds of thousands of file records. Building a complete CSV or JSON document and writing it synchronously from the Walk callback can freeze the UI and retain another full copy of sensitive report data in memory. The Save dialog also returns a typed path separately from its selected format, so adding or changing an extension after the dialog can target a file that was never approved for replacement. A failed or interrupted direct write can leave a partial report.

## Decision

The selected report filter is authoritative. TwinTidy resolves the final extension before authorizing an overwrite and exports an immutable snapshot of the reviewed duplicate groups on a generation-scoped background operation. CSV and JSON are streamed record by record through a context-aware writer to a short, same-directory staging file. The worker syncs and closes the complete staging file, checks cancellation, and atomically publishes it to the authorized destination. Failure or cancellation removes staging and preserves the prior destination.

The GUI accepts completion only for the matching folder revision and operation generation. A close request cancels an active export and waits for its cleanup acknowledgement before allowing the process to exit. CSV data cells that could be evaluated as formulas are neutralized.

## Consequences

### Positive

- Large exports no longer serialize or write the complete document on the UI thread.
- Cancellation and process close do not knowingly strand partial sensitive reports.
- Format, extension, overwrite consent, and final destination stay aligned.
- Existing scan results remain reviewable after export success, failure, or cancellation.

### Negative

- Report export becomes another explicit application-state transition.
- Atomic replacement depends on the destination filesystem's rename behavior; remote or sync-managed providers remain outside TwinTidy's control.
- The GUI must retain a snapshot of reviewed group metadata until the worker finishes.

## Alternatives considered

- **Serialize the complete document before starting a worker:** rejected because it retains the largest allocation and UI-thread delay.
- **Write directly to the final file:** rejected because failure can truncate an existing report or expose a partial new report.
- **Trust only the path checked by the native dialog:** rejected because format normalization can select a different final path.
- **Abandon the writer when the window closes:** rejected because sensitive staging data can remain behind.

## Validation

- Parse streamed CSV and JSON and compare their counts, metadata, estimates, and formula neutralization.
- Inject writer failure and cancellation; verify the old destination remains unchanged and no staging file survives.
- Exercise a near-maximum destination basename to prove staging does not inherit it.
- Test export operation start, cancellation, stale-token rejection, deferred close, and result-control restoration.
- Run the native Windows UI smoke plus the complete amd64 and ARM64 release verification gates.
