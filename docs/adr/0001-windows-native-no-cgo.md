# ADR 0001: Windows-native Go desktop application without CGO

- Status: Accepted
- Date: 2026-07-10

## Context

TwinTidy is a filesystem utility whose value depends on predictable Windows behavior, low deployment friction, and an understandable local trust boundary. The existing implementation uses Go, `github.com/lxn/walk`, and `github.com/lxn/win`. Requiring a browser runtime, background service, C toolchain, or bundled cross-platform GUI runtime would enlarge installation, release, and security scope without improving exact duplicate detection.

The GUI toolkit is mature but old and exposes Windows-specific behavior. This creates maintenance work around COM, manifests, high DPI, and architecture resources, but it also permits a single native executable with direct Windows integration.

## Decision

TwinTidy remains a modular Windows desktop monolith implemented in Go with Walk and Windows API adapters.

Production executables are built with:

```text
GOOS=windows
CGO_ENABLED=0
GOARCH=amd64 or arm64
```

GUI code stays in Windows-specific adapters. Scan, identity, selection, and deletion policy remain independently testable and must not depend on Walk widgets. Long-running work runs outside the UI thread, with UI updates marshaled through Walk synchronization.

The application runs as the current user and requests no elevation for normal operation.

## Consequences

### Positive

- Single-file native deployment is possible.
- No GCC, MinGW, MSYS2, GTK, Electron, or auxiliary service is required.
- Windows Shell, Recycle Bin, file identity, and native accessibility APIs are directly available.
- Local-only processing and privacy claims remain simple to audit.

### Negative

- Windows is the only production platform.
- Walk and its Win32 bindings require careful maintenance and targeted tests.
- Native COM/resource code has architecture and vetting concerns.
- UI modernization must preserve native accessibility and must not blur policy boundaries.

## Alternatives considered

- **Electron or Wails:** easier web UI composition, but materially larger runtime and attack surface; Wails also changes build/runtime requirements.
- **Fyne or another cross-platform toolkit:** broader platform reach, but no current product requirement justifies the CGO/rendering and Windows-integration tradeoffs.
- **CLI-only application:** simpler packaging, but does not provide the review, preview, keeper, and confirmation workflow required for a safety-first consumer tool.
- **Native C#/WinUI rewrite:** strong Windows platform fit, but a full rewrite would discard tested Go scanner work and expand the current scope.

## Validation

- CI builds release-mode executables with `CGO_ENABLED=0` for amd64 and ARM64.
- Dependency inspection confirms no runtime DLL/toolchain requirement beyond Windows.
- Native startup smoke tests run on both supported architectures.
- Policy packages have tests that do not require a Walk window.
