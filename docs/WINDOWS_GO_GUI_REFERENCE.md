# Windows-native Go GUI reference

TwinTidy is the repository's production example for a single-executable Windows desktop application built with the normal Go toolchain.

## Baseline

- GUI framework: `github.com/lxn/walk`
- Windows bindings: `github.com/lxn/win` and `golang.org/x/sys/windows`
- release targets: Windows `amd64` and `arm64`
- build mode: `CGO_ENABLED=0`
- no persistent `go env -w` changes
- no MSYS2, MinGW, GCC, GTK, Electron, local server, or Python runtime

Use this approach when native Windows dialogs, accessibility, Shell integration, compact deployment, and local-only processing matter more than cross-platform UI reach.

## Recommended shape

```text
cmd/appname/main.go
internal/buildinfo/
internal/core/
internal/diagnostics/
internal/gui/app_windows.go
internal/gui/app_stub.go
internal/platform/
docs/ARCHITECTURE.md
docs/adr/
scripts/
```

Keep domain policy independent of Walk. Widget code should translate immutable UI-thread snapshots into core requests and marshal immutable background results back to the UI thread.

## Operation-state rules

- Represent long-running phases explicitly instead of with unrelated booleans.
- Give every asynchronous operation a generation token and scope revision.
- Accept callbacks only inside the synchronized UI-thread callback.
- Cancel and invalidate work when its folder/scope is replaced.
- Derive button, table, and filter enabled state from one phase renderer.
- Never access widgets from worker goroutines.
- Bound preview concurrency and use latest-request-wins invalidation.
- Veto window close while an irreversible or externally owned native operation is in flight; finish applying its result before closing.

## Destructive-action rules

- A highlighted row is navigation/preview state, not destructive intent.
- Use an explicit checkbox or equivalent per-item selection control.
- Confirm the exact count, operation, reversibility, and keeper policy.
- Treat paths as display/location data, never as sufficient authority.
- Revalidate physical identity and content immediately before acting.
- Prefer a reversible platform operation; do not silently fall back to a more destructive operation.
- Verify the platform outcome before removing a row or showing success.
- Preserve failed, changed, skipped, cancelled, and ambiguous results for review.

## Build pattern

Set build configuration only in the current process:

```powershell
$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$env:GOARCH = "amd64"
go test ./... -count=1
go build -trimpath -ldflags "-H=windowsgui" -o AppName.exe ./cmd/appname
```

Generate architecture-specific `.syso` resources from one pinned source configuration. Include Common Controls v6, `asInvoker`, PerMonitorV2, the application icon, and aligned PE version information. Never assume an amd64 object can be linked into ARM64.

## Production checklist

- Architecture, security model, and material decisions are recorded.
- Core behavior, state transitions, stale callbacks, and destructive faults have regression tests.
- Startup failures are visible and produce a non-zero exit status.
- Diagnostics are local, privacy-limited, and accessible to the user.
- `go mod verify`, `go mod tidy -diff`, formatting, vet, tests, and `git diff --check` pass.
- Both supported architectures build with `CGO_ENABLED=0` and include inspected resources.
- Native startup smoke tests pass on x64 and ARM64 Windows.
- Unsigned artifacts reproduce; protected release jobs sign and timestamp final artifacts.
- Release checksums and provenance refer to the exact distributed bytes.

See TwinTidy's [architecture](ARCHITECTURE.md), [security model](SECURITY_MODEL.md), and [release guide](RELEASE.md) for the concrete implementation.
