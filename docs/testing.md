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

Keep a fixture pinned to the single current CPA release (v8.0.12). It should
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

Download the official plugin-enabled CPA v8.0.12 binary for the target platform.
Build the plugin with a C compiler for the same architecture, then run:

```powershell
go build -buildmode=c-shared -o dist/cpa-helper-plugin.dll ./cmd/cpa-helper-plugin
$env:CPA_BINARY = 'C:\path\to\v8.0.12\cli-proxy-api.exe'
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

The v8.0.12 fixture also checks that `/v1/models` contains only the permitted
alias for the restricted caller. Unit contract fixtures cover OpenAI, Claude
cloaking/pagination IDs, Gemini, Codex, group denial precedence, disabled and
unconfigured keys, missing/ambiguous headers, malformed catalogs and policy
failure. Model-list errors are body replacements with HTTP 200, not RPC errors.
The relaxed-list fixture verifies that query authentication and conflicting
headers retain the full catalog, unauthenticated/invalid-key requests still return
401, and generation with the restricted authenticated identity still returns 403.
The toggle fixture patches the official plugin configuration false then true,
waits for CPA's asynchronous reconfiguration through `/capabilities`, checks
version metadata and the resulting catalog, and verifies generation stays denied.
The fixture installs `CPA_PLUGIN_BINARY` under the canonical plugin filename,
so a build artifact may have a trial suffix without changing its host plugin ID.

## Browser verification and preview

Codex streaming rejection acceptance uses `npm run test:codex-verification`.
Set `CODEX_TEST_BINARY` to the installed Codex executable (verified 0.155.1).
It uses the same isolated Docker artifacts as the response UI test, synthetic
credentials and a mock upstream that stays open until cancellation. It checks
immediate mismatch, delayed mismatch and strict unknown rejection: one upstream
request, cancellation, no automatic reconnection and a failed client turn.
`CPA_PLUGIN_BINARY` optionally selects a different library; `--baseline` on the
script reproduces the former retry bug against the old library. The client uses
`--ignore-user-config`, read-only sandboxing and an empty temporary workspace;
no real provider or deployment configuration is used.

The Responses buffering regression additionally sends a complete synthetic tool
call before a late model mismatch, and a matching initial model that changes at
completion. Wire and real Codex checks assert that neither text nor tools escape;
a matched response still completes. `--leak-baseline` reproduces early tool leakage
against the previous library. Unit fixtures cover delimiterless native Codex data
lines, Chat-to-Responses events, fragmented CRLF, buffer limits,
non-Responses admission and per-chunk verification, upstream failure and cancellation. Native delimiterless
frames are tested at the CPA translator callback boundary; the plugin
does not infer missing boundaries from arbitrary byte fragments.

Response-model acceptance uses `npm run test:response-ui`. Prerequisites: Docker
Desktop, the local `golang:1.26-bookworm` image, Edge, and the official v8.0.12
Linux binary at `dist/response-model-trial/cli-proxy-api` with this build's
`cpa-helper-plugin.so` beside it. Verify downloaded release checksums first.
The script creates its own temporary config, synthetic keys, mock upstream and
container, then removes them. It does not replace the running CPA installation.
Screenshots: `test-results/response-model-{desktop,mobile}.png`.
It checks validated panel save, invalid input, refresh, restart persistence,
an actual mismatch error/header and responsive layout.

The Go real fixture additionally checks OpenAI Chat, Responses, Claude and Gemini
HTTP/SSE delivery from a synthetic OpenAI upstream, suppressing mismatched content,
explicit mappings and unknown rejection. Unit tests cover raw-vs-translated
identity, exact matching, lifecycle cleanup, ambiguity, concurrent observation and
transactional reconfiguration. Ordinary upstream traffic is not used.

```powershell
npm ci
$env:CPA_BINARY = 'C:\path\to\v8.0.12\cli-proxy-api.exe'
npm run test:ui
npm run preview
```

`npm run test:directory` checks v8 grouped credentials, host `auth_index`, shared
indexes, duplicate keys, group inheritance, keyless providers, file credentials,
invalid indexes and secret masking. The real CPA fixture verifies the configured
credential ID bridge with actual subset routing; v8.0.12's file directory omits
configured credentials and scheduler hooks do not expose their indexes.
The browser fixture rejects deprecated host API calls and verifies v8 config
PUT, mapping deletion, preservation of sibling settings, MiB-to-byte conversion and reload.
It also loads a legacy `api-key-entries: null` keyless group, asserts the real v8
`keys: null`/group `auth_index` response and successful directory synchronization.
A Claude credential with an own `__proto__` header is selected using the browser's
directory adapter: allow must reach the mock upstream, deny must return a 503
routing error without reaching it. After asserting successful null-list discovery,
the fixture normalizes its list to `[]`: CPA v8.0.12 reads legacy null lists but
rejects them when validating v8 configuration writes. Directory unit tests additionally
cover inherited and per-key prototype-named headers for Claude/Codex and reject
malformed credential lists other than the supported keyless null value.

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
`docs/docker-deployment.md` for the actual v8.0.3 acceptance evidence.

## Platform notes

Windows builds require a current MinGW-w64 UCRT toolchain. The old local MinGW 8.1
produced an unloadable DLL and race executables with missing entry points; compile
success alone was insufficient. Point `CC` and the process-local PATH at UCRT GCC.
The Linux artifact built on Debian bookworm targets glibc systems, not musl/Alpine.
