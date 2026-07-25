# ADR 0009: Identity-verified hardlink consolidation

- Status: Accepted
- Date: 2026-07-24 (proposed and accepted by the product owner the same day)

Number 0008 is reserved by the identity-bound recycle verification spike on
its in-review branch; this ADR is independent of that work but reuses its
retained-handle verification primitive if both are accepted.

## Context

ADR 0005 disables destructive recycling until an identity-bound adapter can
prove that the verified `VolumeSerial + FileId` remains authoritative through
the destructive operation. That gate is correct, but it leaves TwinTidy unable
to reclaim any disk space: users export a duplicate report and delete files by
hand, outside every safety mechanism this project built.

NTFS offers a cleanup mode with a fundamentally different risk profile.
Replacing a duplicate file with a hardlink to the kept copy reclaims the
duplicate's space while the content remains reachable at every original path.
No user data becomes unreachable at any point: the failure mode of a botched
replacement is a leftover extra link or an unchanged file, not a lost file.

The scanner already treats hardlinked files conservatively: records with
`linkCount > 1` are excluded from duplicate matching, so consolidated groups
naturally drop out of future scans rather than being re-reported.

## Decision

Proposed: TwinTidy may offer "consolidate as hardlink" for duplicate groups,
subject to all of the following, each failing closed:

1. **Same-volume constraint.** Hardlinks cannot cross volumes. Members whose
   `VolumeSerial` differs from the keeper's are excluded from consolidation
   with a visible reason, never silently.
2. **Identity verification before and after.** The keeper and each candidate
   are re-verified against their captured `VolumeSerial + FileId` through open
   handles immediately before the operation. The replacement sequence is:
   create the new link under a temporary name in the candidate's directory,
   re-verify the candidate's identity, atomically replace the candidate with
   the link (`ReplaceFileW` semantics), then re-verify that the final path now
   resolves to the keeper's identity. Any mismatch aborts before the replace
   step; an abort after link creation removes only the temporary link.
3. **Selection invariant.** Consolidation consumes the keep-rules selection
   (one keeper always retained per group) and refuses any request that would
   consolidate every member of a group.
4. **Explicit user consent per run.** Consolidation is a distinct action with
   its own confirmation that names the semantic change: after consolidation,
   editing the file at one path edits it at every consolidated path. This
   caveat is surfaced in the confirmation, the result report, and the
   documentation, because it is the one real behavior change users can
   observe.
5. **Excluded content stays excluded.** Files with named streams, protected
   extensions, or paths outside the authorized scan scope are never
   consolidation candidates, mirroring the existing matching exclusions.
6. **Adversarial coverage before release.** The native smoke matrix gains
   fixtures for: success, cancellation mid-batch, path swap between
   verification and replace, cross-volume rejection, read-only target,
   open-handle target, and abort-cleanup of the temporary link.

This ADR does not supersede ADR 0005. Recycling remains disabled; hardlink
consolidation is a separate, reversible-by-construction operation. Whether it
may ship before ADR 0005 is superseded is exactly the decision this proposal
puts to the product owner: the operation modifies the filesystem, but no
content becomes unreachable, and every path continues to resolve to the same
bytes.

## Consequences

### Positive

- Users reclaim duplicate space with zero risk of losing content, inside
  TwinTidy's verification model instead of by hand outside it.
- The failure modes (leftover temp link, unchanged file) are inspectable and
  recoverable, unlike a wrong deletion.
- Consolidated groups self-remove from future scan results via the existing
  `linkCount > 1` exclusion.

### Negative

- Post-consolidation, an edit through any path is visible at all paths. Users
  who expected independent copies can be surprised; the consent flow exists
  for this reason, and backup tools that are hardlink-unaware may still copy
  each path's content.
- Space accounting becomes less intuitive: deleting one link does not free
  space until the last link is gone.
- A future recycle implementation must account for consolidated files: the
  `linkCount > 1` exclusion currently guarantees they are never candidates.

### Acceptance record

- The product owner accepted this ADR on 2026-07-24 and authorized
  implementation ahead of any ADR 0005 supersession: hardlink consolidation
  may ship while recycling remains disabled, because no content ever becomes
  unreachable.
- The implementation replaces the `ReplaceFileW` sketch with a sequence that
  is identity-bound at every destructive step, eliminating the
  path-race window that motivated the ACL follow-up: the candidate is opened
  with delete access and its verified handle performs the rename-away
  (`FileRenameInfo`) and final disposal (`FileDispositionInfo`); the original
  path is then relinked to the keeper and re-verified against the keeper's
  identity before the backup is disposed. A failure at any step leaves the
  content reachable (at the original path or the backup path) and reports the
  backup location instead of guessing.
- The adversarial fixtures listed in the Decision section gate the
  implementation change; the cross-volume rejection fixture runs where a
  second volume is available and the remaining fixtures run in the standard
  suite.
