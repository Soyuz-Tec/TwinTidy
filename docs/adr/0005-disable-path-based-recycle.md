# ADR 0005: Disable path-based Windows recycling

- Status: Accepted
- Date: 2026-07-12

## Context

ADR 0002 requires a destructive Recycle Bin action to remain bound to the exact file identity that TwinTidy revalidated through an open handle. The initial Windows adapter retained that handle, but passed only the pathname to `SHCreateItemFromParsingName` and `IFileOperation::DeleteItem`. A concurrent rename and replacement could therefore cause the Shell to recycle a different object. Inspecting the retained handle afterward detected the mismatch but could not prevent or undo the wrong action.

A disposable Windows experiment also proved that creating and retaining the `IShellItem` before the swap does not bind it to the original file ID: `IFileOperation` recycled the later replacement at the parsing-name path. The documented Shell APIs expose no recycle-by-handle operation or file-ID stability guarantee for a Shell item.

## Decision

The production Windows recycle adapter is disabled and returns a clear unsupported safety result before any destructive native call. The GUI keeps cleanup controls disabled and explains that no file will be changed. Scanning, exact hashing, file-identity capture, keeper policy, selection planning, previews, and test-injected policy validation remain available.

Cleanup may be enabled only when a reversible implementation can prove that the verified `VolumeSerial + FileId` remains authoritative through the destructive operation. It must preserve the ADR 0002 keeper, scope, outcome, and no-permanent-fallback guarantees. Path staging, progress callbacks, or post-operation receipts are insufficient if another process can still replace the final path occupant.

## Consequences

### Positive

- A path swap cannot make the production build recycle an unverified file.
- Safety claims, runtime behavior, and tests agree.
- Duplicate discovery and review can continue while the destructive boundary is redesigned.

### Negative

- The current pre-release build cannot reclaim space directly.
- A future identity-aware reversible backend may require a trusted broker, a supported platform capability not currently exposed, or a narrower explicitly approved threat model.

## Alternatives considered

- **Keep the path-based adapter and verify afterward:** rejected because detection after deletion does not prevent harm.
- **Pre-create an `IShellItem` or use a PIDL:** rejected because neither has a documented file-ID binding, and the `IShellItem` approach failed the swap experiment.
- **Rename to a random staging path before calling the Shell:** rejected as a complete fix because the final path handoff remains replaceable by a same-user process.
- **Use permanent handle-based deletion or manually write Recycle Bin internals:** rejected because permanent deletion violates user intent and the `$I`/`$R` store is not a supported public contract.

## Validation

- The production adapter reports recycling as unsupported without changing the target or keeper identity.
- Scope-replacement and ancestor-junction tests prove that destructive policy fails before the adapter.
- Any future replacement must add a barrier-controlled native swap test in which a replacement file's identity and bytes always survive, plus native Recycle Bin receipt, cancellation, failure, and rollback evidence.
