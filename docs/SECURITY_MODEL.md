# TwinTidy Security Model

Status: production security contract, 2026-07-10

## Security objective

TwinTidy must help users remove verified redundancy without creating a new path to data loss, arbitrary file access, code execution, privacy leakage, or untrusted software distribution.

## Assets

- User files and the continued availability of at least one verified copy
- File paths, metadata, content hashes, and duplicate-group information
- User intent: selected roots, chosen keepers, and confirmed recycle targets
- Exported reports and their same-directory staging files
- Local diagnostics and crash reports
- Release binaries, manifests, checksums, provenance, and signing identity
- Maintainer credentials and GitHub workflow permissions

## Trust boundaries

1. **Selected filesystem roots:** filenames and file contents are attacker-controlled input, including files synchronized or replaced by another process.
2. **Reparse points and network/sync providers:** path text may not identify the final object and the object may change asynchronously.
3. **Windows Shell, COM, browser, and preview handlers:** external system components parse untrusted content and can fail, hang, or navigate unexpectedly.
4. **Recycle service:** currently disabled at the production adapter; any future implementation must interpret native results, cancellation, file identity, and source-path state.
5. **Diagnostics boundary:** support information leaves the process and may later be shared publicly.
6. **Report-export boundary:** a report discloses paths, hashes, and metadata to a user-chosen destination; sync clients or network providers may transfer it under their own configuration.
7. **Build and release supply chain:** source, dependencies, actions, build workers, artifacts, signing identity, and release permissions have different trust levels.

## Threats and required controls

| Threat | Required controls |
|---|---|
| File replaced after scan | Carry expected file identity and hashes; revalidate immediately before recycling; skip on any mismatch |
| Last verified copy selected | Keeper policy rejects the request by default; confirmation lists retained and targeted paths |
| Junction/symlink escapes protected scope | Inspect reparse points, validate final targets, avoid link traversal by default, test selected-root aliases |
| Hard links misreported as duplicates | Compare stable file identities and do not count multiple paths to one object as reclaimable copies |
| Alternate data streams lost during comparison | Detect named streams from the open file handle and protect the file from exact-duplicate cleanup |
| Stale operation mutates new results | Generation-scoped operations, cancellation, and current-generation checks on every callback |
| Native recycle acts on a swapped path occupant | Disable path-based Shell recycling. A future adapter must keep the verified file identity authoritative through the action, prove that exact identity reached the Recycle Bin, and then verify the source-path outcome |
| Automatic permanent deletion after recycle failure | Prohibited; preserve the file and report an actionable failure |
| Cancellation ignored during hashing | Context-aware bounded read loop and cancellation tests with slow/large files |
| Malicious preview content | Bounded reads, constrained WebView navigation, no macro execution, explicit open action for risky formats |
| Resource exhaustion | Streaming hashes, bounded buffers/previews, concurrency limits, cancellation, and progress visibility |
| Sensitive diagnostics | No file contents, environment dumps, credentials, or silent uploads; warn users to review logs before sharing |
| Sensitive report disclosure | Export only after an explicit user action; state which paths, hashes, and metadata are included; write only to the chosen filesystem destination; warn that sharing or choosing a synced, cloud-backed, or network destination can disclose the report through that provider |
| Spreadsheet formula injection through CSV | Neutralize data cells whose first effective character could be evaluated as a formula, including after leading whitespace or control characters; cover hostile filenames and paths in tests |
| Report saved over an unintended file | Make the selected format authoritative, resolve the final path and extension before authorization, prompt against that exact resolved path, and never infer overwrite authority from a different path shown earlier in the dialog |
| Partial or stale report publication | Stream on a cancellable background worker to a short, same-directory staging file; sync, close, and atomically replace only after success; remove staging files after failure or cancellation |
| Dependency/action compromise | Minimal dependencies, `go mod verify`, vulnerability review, full-SHA action pins, Dependabot, CodeQL, protected branches |
| Release substitution | Clean exact-commit build, source/executable receipt, digest-pinned packaging, reproducible unsigned build, provenance, Authenticode signatures, SHA-256 checksums, protected tag release |

## Authorization model

TwinTidy runs with the current user's permissions and requests no elevation. Selecting a folder authorizes read-only inspection within the validated scope. It does not authorize deletion or report export. Export requires a separate user action, format choice, and destination choice. Overwriting is authorized only for the exact resolved path after format and extension normalization. The current build has no enabled destructive action. A future recycle action requires a separate, explicit confirmation that identifies each target and retained copy.

Confirmation is bound to expected file identities. A stale confirmation cannot be reused for a different filesystem object at the same path.

## Privacy model

- All scanning and matching is local.
- No telemetry, analytics, update request, account, or cloud upload occurs by default.
- Logs stay under `%LOCALAPPDATA%\TwinTidy\logs` unless the user deliberately shares them.
- Diagnostics record operational state needed for support, not file contents.
- Reports are written only to an explicitly chosen filesystem destination. TwinTidy has no report-upload client, but a user-selected synced, cloud-backed, or network destination may be transferred by its configured provider.
- A future network feature requires explicit opt-in, a privacy update, threat-model revision, and ADR.

## Release security

GitHub workflows use least-privilege token permissions and pinned action commits. Pull-request workflows do not receive signing credentials. Signing executes only for protected release tags in a protected environment with approval, and secrets must never be written to logs or artifacts.

Unsigned artifacts are built from the exact resolved commit and reproducibility-checked before signing. A machine-readable receipt binds source-tree identity to every executable digest, and packaging accepts only independently captured executable/receipt hashes while holding the input files immutable. Both executables and installers are signed and timestamped, then verified before release. Checksums are calculated from the final distributed bytes.

## Security validation

Before enabling cleanup or issuing a stable release that includes cleanup, verify at minimum:

- replacement, truncation, rename, path swapping, and content mutation between scan and recycle
- hard links, alternate data streams, symlinks, junctions, protected roots, UNC paths, and unavailable network paths
- recycle cancellation, access denied, locked file, provider delay, partial result, and ambiguous native result
- every-copy selection, mixed groups, duplicate paths, stale confirmation, and operation-generation races
- large and continuously changing file cancellation
- preview navigation and malformed file behavior
- clean amd64 and ARM64 startup with embedded resources
- dependency, CodeQL, signature, checksum, and provenance gates; installer lifecycle gates when an installer is distributed

Security reports follow the repository-level `SECURITY.md`. Do not publish exploit details in a public issue.
