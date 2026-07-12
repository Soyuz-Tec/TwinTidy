# Changelog

All notable TwinTidy changes will be documented here. The project uses [Semantic Versioning](https://semver.org/) once versioned releases begin.

## Unreleased

### Added

- TwinTidy product identity, native icon, build information, architecture records, security model, and release guardrails.
- Windows amd64 and ARM64 resource/build/package targets.
- Stable Windows file identities, hard-link/alternate-stream protection, reparse-safe traversal, and cancellable hashing.
- Deterministic GUI operation generations and checkbox-only cleanup intent.

### Changed

- Renamed the module, command, executable, diagnostics directory, and user-facing application from Duplicate File Finder Go to TwinTidy.
- Pinned the supported Go patch toolchain and made startup/smoke failures visible through process exit status.

### Security

- Added group-aware pre-action revalidation and disabled path-based Windows Recycle Bin calls until the verified file identity can remain authoritative through the native operation.
