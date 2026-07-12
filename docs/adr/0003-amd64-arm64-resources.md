# ADR 0003: Generate Windows resources for amd64 and ARM64

- Status: Accepted
- Date: 2026-07-10

## Context

Walk requires the Windows Common Controls v6 manifest for reliable startup and widget behavior. A `.syso` resource object is architecture-specific even when its XML manifest is architecture-neutral. The original repository contained only an amd64 resource, allowing other targets to compile without the required manifest.

TwinTidy also needs a product icon and PE version information. Relying on a developer machine's Windows SDK would conflict with the normal-Go-toolchain build goal and make resource generation harder to reproduce.

## Decision

TwinTidy supports Windows `amd64` and `arm64`. Windows `386` is unsupported.

The repository keeps one source configuration for:

- Common Controls v6 and `asInvoker` manifest behavior
- PerMonitorV2 DPI behavior
- product icon
- TwinTidy PE version strings and fixed version fields

Pinned pure-Go `github.com/tc-hib/go-winres@v0.3.3` produces:

```text
cmd/twintidy/rsrc_windows_amd64.syso
cmd/twintidy/rsrc_windows_arm64.syso
```

Generated files are reproducible and never edited manually. Checked-in objects contain development metadata. Versioned builds create a temporary copy of the current source, generate semantic-version-specific resources there, link from that isolated copy, and remove it without modifying tracked development resources. The normal release link includes the matching `.syso`; post-link manifest patching is not part of the canonical build. CI regenerates development resources and detects drift, then builds and inspects both executables.

## Consequences

### Positive

- Both supported architectures receive identical manifest, icon, and version intent.
- Resource generation does not require `rc.exe`, `cvtres.exe`, MinGW, or CGO.
- A clean checkout can reproduce resource objects and executable metadata.
- Missing architecture resources become a CI failure rather than a runtime surprise.

### Negative

- The generator is a pinned build dependency that must be reviewed and updated.
- Semantic prerelease versions require a documented mapping to four-part PE numeric versions.
- Generated binary resources require extraction-based review rather than text diff alone.

## Alternatives considered

- **Post-build `mt.exe` embedding:** valid but requires the Windows SDK, modifies the executable after linking, and does not by itself solve icon/version resources.
- **`rc.exe` plus `cvtres.exe`:** Microsoft-native but adds SDK/MSVC discovery and architecture-specific build-host requirements.
- **Keep amd64 only:** rejected because Windows ARM64 is an explicit production target.
- **Generate a 386 resource:** rejected because 32-bit Windows is not a supported product target.

## Validation

- Regenerate resource objects twice and compare SHA-256.
- Build clean amd64 and ARM64 executables with `CGO_ENABLED=0`.
- Extract and verify manifest, icon, ProductName, FileVersion, ProductVersion, and OriginalFilename.
- Run actual GUI startup smoke tests on native x64 and native Windows ARM64.
