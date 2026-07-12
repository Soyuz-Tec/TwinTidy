# ADR 0002: Revalidate identities and verify recycle outcomes

- Status: Accepted target; native activation constrained by ADR 0005
- Date: 2026-07-10

## Context

A duplicate result is a point-in-time observation. Editors, sync clients, network providers, and other processes can replace or modify a file after scanning. A path can then refer to different content while looking unchanged to the user.

Recycle APIs can also fail, partially complete, or be cancelled. A wrapper that returns no Go error is not sufficient proof that Windows recycled a file. Automatically falling back to permanent deletion turns a reversible user request into an irreversible operation under the least reliable condition.

## Decision

Every recycle candidate carries an expected identity contract from the verified scan. Immediately before the native operation, TwinTidy revalidates:

- canonical/validated path and scope
- stable Windows file identity when available
- regular-file and reparse-point policy
- exact size and required modification metadata
- full exact-content hash
- membership in a duplicate group with at least one separate retained identity

Any mismatch, read failure, missing file, ambiguous identity, or missing keeper produces a skipped or failed result. It never authorizes recycling another object at the same path.

A conforming Windows adapter must use a native recycle mechanism that exposes the primary result and cancellation/aborted state. It must keep the expected object handle authoritative across the operation and issue a receipt only when that exact file identity reaches the volume-root `$Recycle.Bin`. Success additionally requires post-operation verification that the original source path is absent. ADR 0005 records that the documented path-based Shell mechanism cannot currently meet this target, so the production adapter remains disabled.

There is no automatic permanent-delete fallback. A future permanent-delete feature would be a separate command, confirmation, threat-model update, and ADR.

Results are structured per target as recycled, skipped-changed, cancelled, or failed. The UI retains uncertain/failed rows and shows exact reasons. Audit diagnostics record outcomes without file contents.

## Consequences

### Positive

- Scan-to-delete races fail closed.
- Recycle failure cannot be presented as success merely because a wrapper returned `nil`.
- User intent remains reversible by default.
- Keeper preservation becomes testable policy rather than UI convention.

### Negative

- Full rehashing before recycling adds I/O and time.
- Native Windows adapter code and fault injection are required.
- Network and cloud-backed files may be skipped more often when identity or outcome cannot be proven.
- Post-operation verification must distinguish an expected moved object from a new object rapidly created at the same path.

## Alternatives considered

- **Trust path, size, and modification time:** rejected because an object can be replaced while preserving superficial metadata.
- **Trust a generic trash wrapper:** rejected unless it exposes and correctly interprets the native result and cancellation state.
- **Delete permanently when recycle fails:** rejected because it violates reversible user intent precisely when the safer mechanism is uncertain.
- **Require only user confirmation:** rejected because confirmation cannot establish current filesystem identity.

## Validation

Automated tests cover replacement, same-size content mutation, timestamp preservation attempts, hard links, alternate data streams, missing keeper, locked files, access denied, recycle cancellation, path swapping, expected-object receipts, ambiguous native results, source-path verification, and stale operation generations. A stable release also receives an independent destructive-workflow review.
