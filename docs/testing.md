# Testing and Verification

## Unit tests

- Policy precedence: deny over allow, independent allow/deny unions, disabled keys.
- Snapshot validation: malformed, stale, duplicate, and unsupported-version inputs.
- Atomic replacement and rollback after a failed activation.
- Request lifecycle: reservation release on success, error, cancellation, and timeout.
- Secret hygiene: logs and serialized management responses contain identifiers only.

## Contract tests

Use `httptest` for generic management calls. The suite must verify:

1. An administrator can read capabilities and health.
2. A valid snapshot is accepted and becomes active.
3. A stale or invalid snapshot is rejected without changing the active revision.
4. The administrator observes the accepted revision and last synchronization error.
5. Restarting the plugin reloads the last valid local snapshot.

## CPA compatibility fixture

Keep a small fixture or test harness pinned to each supported CPA version. It should
exercise the plugin entry point, hook registration, caller metadata, model decision,
completion cleanup and official management resource registration. V1 does not
register a usage callback.

## Required commands

```powershell
go fmt ./...
go vet ./...
go test ./...
go test -race ./...
```

Run the CPA fixture and management contract tests before changing `docs/compatibility.md`
to mark a release supported.

## Real CPA fixture and management smoke test

Download the official plugin-enabled CPA v7.2.143 binary for the target platform.
Build the plugin with a C compiler for the same architecture, then run:

```powershell
go build -buildmode=c-shared -o dist/cpa-helper-plugin.dll ./cmd/cpa-helper-plugin
$env:CPA_BINARY = 'C:\path\to\v7.2.143\cli-proxy-api.exe'
go test -v ./integration -count=1 -timeout 90s
```

Linux uses `dist/cpa-helper-plugin.so` and `CPA_BINARY=/path/to/cli-proxy-api`.
`CPA_PLUGIN_BINARY` optionally overrides the dynamic library path. Without
`CPA_BINARY`, the integration test explicitly skips; a normal `go test ./...`
pass alone does not certify CPA compatibility.

The fixture creates an isolated temporary CPA config, state directory, loopback
port and mock OpenAI-compatible upstream. It never reads deployment config or
real credentials. It checks management authentication, static resources, aliases,
3:1 subset weights, 403/429/503 behavior, streaming cancellation, restart and
rollback. The test stops its CPA process and removes temporary state at completion.
CPA itself can still perform its own background version checks.

## Browser verification and preview

```powershell
npm ci
$env:CPA_BINARY = 'C:\path\to\v7.2.143\cli-proxy-api.exe'
npm run test:ui
npm run preview
```

Browser verification uses installed Microsoft Edge through Playwright. It starts
its own CPA instance and mock upstream, then checks encoded CPA session reuse,
key masking, directory-only category choices, directory loading,
rule creation, optional multi-line rule notes, tri-state selection, binding, concurrency editing, explicit saves,
failed saves, reload, credential storage and desktop/mobile layout. Screenshots are written
to ignored `test-results/`. Preview prints its isolated URL and test management key;
stop with Ctrl+C. The plugin now requires an existing CPA panel session; the preview
is only an isolated server fixture. Neither command touches an existing CPA installation.

`scripts/live-ui-smoke.mjs` uses `CPA_URL` and `CPA_MANAGEMENT_KEY` to log into the
actual official CPA management page in a fresh Edge context. It checks the plugin
iframe, automatic session reuse after reload, masked downstream/upstream key display
and inventory-only categories. It performs no policy writes or screenshots of real
credentials. CPA Remember Password is enabled only inside that disposable context.

## Existing Docker service smoke test

`scripts/live-smoke.mjs` targets an existing service using `CPA_URL` and
`CPA_MANAGEMENT_KEY` from the process environment. It requires an empty plugin
policy, verifies the official menu, creates a temporary downstream key, denies one
registered model, and checks the actual `403/model_forbidden` response. Cleanup
removes the temporary key and rolls the policy back under revision preconditions.
It never sends a permitted generation request. Policy revisions and audit logs
advance. Do not run during concurrent management edits. See
`docs/docker-deployment.md` for the actual v7.3.7 acceptance evidence.

## Platform notes

Windows builds require a current MinGW-w64 UCRT toolchain. The old local MinGW 8.1
produced an unloadable DLL and race executables with missing entry points; compile
success alone was insufficient. Point `CC` and the process-local PATH at UCRT GCC.
The Linux artifact built on Debian bookworm targets glibc systems, not musl/Alpine.
