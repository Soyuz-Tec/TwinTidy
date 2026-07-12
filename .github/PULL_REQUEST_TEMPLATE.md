## Outcome

<!-- What user-visible or engineering outcome does this change deliver? -->

## Scope

<!-- List the focused files/components changed. Call out anything intentionally excluded. -->

## File-safety and security impact

- [ ] No destructive or trust-boundary behavior changes
- [ ] Destructive/trust-boundary behavior changed and the tests, security model, and ADRs are updated

Explain how keeper preservation, pre-action identity verification, recycle-result verification, cancellation, diagnostics privacy, and operation isolation are affected.

## Architecture impact

- [ ] No material architecture impact
- [ ] `docs/ARCHITECTURE.md` and applicable ADRs are updated

## Verification

<!-- Include exact commands and results. Do not write only "tests pass." -->

```text
go mod verify
go mod tidy -diff
go vet ./...
go test ./... -count=1
```

- [ ] amd64 build/resource/startup checks passed or are not applicable
- [ ] ARM64 build/resource/startup checks passed or are not applicable
- [ ] User-facing behavior was smoke-tested or is not applicable

## Risk and rollback

<!-- Describe plausible failures, detection, and how to return users to a safe state. -->

## Release notes

<!-- State the changelog/release-note text, or explain why none is needed. -->
