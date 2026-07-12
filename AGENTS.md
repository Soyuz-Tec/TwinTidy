# TwinTidy Repository Working Agreements

These instructions apply to the entire repository. A more specific `AGENTS.md` may add stricter rules for its subtree, but it must not weaken the safety requirements below.

## Sources of truth

1. Confirm the repository root, active branch, target runtime, and Git state before editing.
2. Treat tests, repository files, GitHub state, and observed Windows behavior as stronger evidence than chat history.
3. Use `docs/ARCHITECTURE.md` for system boundaries and `docs/SECURITY_MODEL.md` for trust boundaries and safety requirements.
4. Record material boundary, deletion, identity, security, packaging, or release decisions in `docs/adr/`.

## Non-negotiable file-safety invariants

- A path alone is never sufficient authority to remove a file.
- Revalidate a deletion candidate against the scanned identity immediately before acting. At minimum compare the platform file identity, size, modification metadata, and exact content hash defined by the deletion contract.
- Skip and clearly report any candidate that changed, disappeared, became unreadable, or no longer belongs to the verified duplicate group.
- Preserve at least one verified copy in every duplicate group by default.
- Recycle is the normal destructive action. Never fall back to permanent deletion automatically.
- The production recycle adapter is currently disabled by ADR 0005. Do not re-enable a path-based Shell call or present cleanup as available without an identity-bound design and its required evidence.
- Treat cancellation, an aborted shell operation, an ambiguous native return value, or an unverifiable outcome as failure.
- Verify the source path outcome after a recycle operation before reporting success or removing the row from the UI.
- Folder changes, reset, scan, and delete operations must be generation-scoped so stale background callbacks cannot mutate newer state.
- Do not follow symlinks, junctions, or reparse points across a protected boundary without an explicit, tested policy.

Any proposal to weaken these invariants requires explicit product-owner approval, a security review, and an ADR.

## Architecture and implementation

- Keep Windows/UI adapters separate from scanning, selection, and deletion policy.
- Keep long-running filesystem work off the Walk UI thread. Marshal UI changes through the window synchronization mechanism.
- Prefer small, reviewable changes. Do not mix broad visual rewrites with filesystem-safety changes.
- Validate all paths and filesystem state at the trust boundary. Do not hard-code secrets or collect file contents in diagnostics.
- TwinTidy is local-first and has no telemetry by default. A future network feature requires an ADR, privacy update, and explicit opt-in.
- Supported production architectures are Windows `amd64` and `arm64`. Windows `386` is unsupported.
- Release binaries must use `CGO_ENABLED=0`. Set it per process or CI job; never change a developer's global Go configuration with `go env -w`.

## Required verification

Run the relevant subset during development and the complete set before publishing:

```powershell
go mod verify
go mod tidy -diff
go vet ./...
go test ./... -count=1
git diff --check
```

Also verify both Windows architecture builds, their embedded manifests and version resources, and a native startup smoke test before a release. A destructive-workflow change requires adversarial tests for replacement-after-scan, recycle failure or cancellation, keeper preservation, and operation-state isolation.

## Generated resources and releases

- Treat the manifest, icon, and version-resource configuration as source; architecture-specific `.syso` files are generated outputs that must be reproducible.
- Do not hand-edit a generated `.syso` file.
- Build unsigned artifacts reproducibly first. Code signing happens afterward and changes the bytes.
- Never expose signing material in the repository, logs, workflow artifacts, or pull requests.
- Publish through a reviewed branch and protected tag workflow. Report exact checks, artifacts, signatures, and smoke environments.
