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

Keep the SDK pinned to v8.0.45 and run the fixture against each explicitly verified
Host (the original v8.0.45 and the v8.0.47 negotiation regression). It should
exercise the plugin entry point, hook registration, caller metadata, model decision,
completion cleanup and official management resource registration. The working tree
removes the usage callback introduced by the historical 0.1.10 TTL feature.
Registration must not advertise `usage_plugin`; direct `usage.handle` returns 404.

`TestRegistrationNegotiatesHostMaximumSchema` covers registration and reconfigure
with host maxima 0/1/6/7/8/9/100/65535/uint32-max and a synthetic unknown registration
field. It rejects hosts below 7 without changing policy state,
keeps the plugin-declared schema at 7 for newer hosts, preserves both body-omission
flags, reports that same schema through capabilities, and cannot enable unused
raw-body patch or removed response hooks. This negotiation matrix does not certify
unknown future CPA releases. The schema belongs to the adapter, independently of
the imported SDK's latest schema. Use an isolated modfile to build this same test
with SDK v8.0.47 (whose latest schema is 8) and confirm Host 7 is still accepted;
keep the repository's pinned go.mod/go.sum unchanged for that regression.

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

TTL production/feasibility fixtures and the diagnostic probe have been removed
with the feature. Historical results remain in [the old feasibility record](claude-ttl-feasibility.md).
Current removal regressions cover legacy YAML, no usage capability/callback,
absent management/UI/health surfaces, no request/header mutation, CPA scheduler
delegation, and no metadata IO. Stored-policy tests cover old canonical receipt
digests, unchanged startup bytes, replay/conflict/new-TTL rejection, retained
permission/concurrency rules, rollback, subsequent normal writes and damaged
state. See [the removal record](claude-ttl-removal.md).

Download the official plugin-enabled CPA binary for the target platform. Use
v8.0.47 to reproduce/check the schema negotiation defect and rerun on v8.0.45 to
protect the pinned baseline; set `CPA_BINARY` separately for each run.
Build the plugin with a C compiler for the same architecture, then run:

```powershell
go build -buildmode=c-shared -o dist/cpa-helper-plugin.dll ./cmd/cpa-helper-plugin
$env:CPA_BINARY = 'C:\path\to\v8.0.45\cli-proxy-api.exe'
go test -v ./integration -count=1 -timeout 90s
```

Linux uses `dist/cpa-helper-plugin.so` and `CPA_BINARY=/path/to/cli-proxy-api`.
`CPA_PLUGIN_BINARY` optionally overrides the dynamic library path. Without
`CPA_BINARY`, the integration test explicitly skips; a normal `go test ./...`
pass alone does not certify CPA compatibility.

Run the compatibility/management fixture against both official Hosts:

```powershell
go test ./integration -run '^TestCPACompatibility$' -count=1 -timeout 240s
```

Run `npm run test:ui` with the fixed DLL and official v8.0.47 as well. The published
v0.1.10 DLL is the negative fixture and must still reproduce `CPA schema 7 is required`
on v8.0.47; do not overwrite release assets to make it appear fixed. Captured source,
checksums and results are in [the fix record](cpa-v8.0.47-schema-negotiation.md).

The fixture creates an isolated temporary CPA config, state directory, loopback
port and mock OpenAI-compatible upstream. It never reads deployment config or
real credentials. It checks management authentication, static resources, aliases,
3:1 subset weights, 403/429/503 behavior, streaming cancellation, restart and
rollback. The test stops its CPA process and removes temporary state at completion.
CPA itself can still perform its own background version checks.

The current regression fixture verifies model detail filtering (allowed 200,
hidden 404, unrestricted caller 200), and response passthrough despite legacy
enabled/reject verification settings. Chat, Responses, Claude and Gemini retain
the upstream output; missing or changed declared models are not rejected.
Responses text must arrive before upstream completion, including Codex-originated
requests. Responses fixtures distinguish clean EOF after finish_reason
(`response.completed`) from truncated EOF (explicit error, no false completion).
Both paths must release request accounting; client cancellation is also covered.
Unit fixtures ensure legacy settings cannot restore raw/stream hooks, both
request bodies are omitted, removed management/RPC methods return 404, and
capabilities, health and registration no longer advertise verification.
Terminal HTTP flush failures are not injected by this fixture.

The current fixture also checks that `/v1/models` contains only the permitted
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

The current fixture checks `GET /v8/management/plugins` configuration field
types/defaults against the plugin's omitted settings. It also disables and
re-enables both the individual plugin and the global plugin system through the
v8 config API. Disabled instances must become unregistered and lose their
management/resource routes; re-enabled instances must recover the identical
persisted policy, model denial, credential subset routing and request accounting.
For both disable paths, an admitted stream ends while the plugin is detached;
re-enabling must admit a new request without a stale concurrency reservation.
Unit tests distinguish final RPC/ABI shutdown cleanup from temporary quiesce,
which must retain in-flight accounting for replacement rollback.

The v8.0.45 credential-catalog regression reads actual registered public aliases
through the plugin `/credential-models` backed by `host.auth.models`, publishes the
versioned plugin `/model-catalog`, and verifies explicit failure before first
sync, shared-model visibility, an empty catalog and detail 404 when all owners
are denied, restart persistence, and unchanged 3:1 subset weights. Unit tests
cover exclusive/shared owners, category and individual denial precedence,
disabled owners, every supported list format, group merges, policy hot updates,
missing/incomplete directories, immutable indexes, transactional persistence,
durable receipts, corruption, and uncertain durability. These reads make no
upstream requests and reserve no concurrency. The real fixture also reloads a
valid format-1 0.1.11/v8.0.45, 0.1.10/v8.0.45, 0.1.9/v8.0.45 and 0.1.8/v8.0.32 catalogs, checks unchanged disk bytes and provenance,
publishes revision 8 after revision 7, restarts, replays the old durable receipt,
and verifies that policy state is unchanged. Unit fixtures cover old-envelope
rejection on new writes, stale/conflicting retries and failed/uncertain upgrade writes.

The Edge fixture checks automatic management-session catalog sync, the running
CPA version from `X-CPA-VERSION` while preserving the pinned catalog envelope,
explicit missing-version display and restoration when the header returns,
an unusable saved catalog without a producer and its retained error details,
absence of contract/catalog revision/catalog status UI rows, registered public
prefixes, exclusions, file/config/keyless/disabled credentials, actual
relative file IDs and distinct IDs sharing an auth_index. Failed Host reads and
model-set changes during double-read do not partially publish a catalog. Directory unit tests
verify actual file/config auth IDs instead of auth_index, rejection of malformed
model responses, and absence of secrets, auth IDs and display data in snapshots.
Generation admission remains the authoritative restriction; no live DSH profile
or external provider credentials are used by these fixtures. Opening the panel
and ordinary sync send zero source-refresh requests. Explicit refresh is tested
against the real local-model route, plus refresh failure and post-refresh sync
failure; each click sends one POST and preserves the last accepted directory.
Separate failed-save tests retain Key and concurrency drafts; TTL controls and
management/capabilities are absent even with enabled legacy YAML.
The post-review fixture also creates an AIStudio runtime-only credential over
`/v1/ws`, disables it through the official status route, and checks that CPA hides
its metadata while the Host retains its empty model set. Synchronization excludes
that non-owner, preserves all known entries and shows the exclusion count after
ordinary sync and the final refresh notice, plus the health area. Changes to the
excluded entry's index or empty-to-nonempty model set between reads must preserve
the old catalog. Directory regressions additionally cover invalid/duplicate
model-free entries, known metadata mismatches, missing Host coverage and all
excluded-identity additions/removals/changes.

## Browser verification and preview

Response verification and its dedicated `test:response-ui` and
`test:codex-verification` scripts have been removed. The ordinary Edge smoke starts
with legacy enabled/reject YAML and verifies successful loading, absence of the
editor/module/settings and a 404 from the removed validation route. Reload must
not restore the feature or rewrite legacy YAML. No external provider is used.

```powershell
npm ci
$env:CPA_BINARY = 'C:\path\to\v8.0.45\cli-proxy-api.exe'
npm run test:ui
npm run preview
```

`npm run test:directory` checks v8 grouped credentials, host `auth_index`, shared
indexes, duplicate keys, group inheritance, keyless providers, file credentials,
invalid indexes and secret masking. The real CPA fixture verifies the configured
credential metadata bridge against the actual Host model fingerprints and subset
routing. v8.0.45's inventory omits some configured/disabled runtime credentials;
the model callback lacks source/status. Neither can silently replace the other.
The browser fixture rejects deprecated host API calls and verifies v8 config
PUT, mapping deletion, preservation of sibling settings, MiB-to-byte conversion and reload.
It also loads a legacy `api-key-entries: null` keyless group, asserts the real v8
`keys: null`/group `auth_index` response and successful directory synchronization.
A Claude credential with an own `__proto__` header is selected using the browser's
directory adapter: allow must reach the mock upstream, deny must return a 503
routing error without reaching it. After asserting successful null-list discovery,
the fixture normalizes its list to `[]`: CPA v8.0.45 reads legacy null lists but
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
and inventory-only categories. Opening the plugin synchronizes its model catalog.
It performs no policy writes or screenshots of real
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
Keep the entire compiler directory in a path without spaces: WinLibs GCC 16.2.0
expands 8.3 aliases before passing its manifest-object path to the linker, so a
short alias alone does not fix race-test linking from a directory with spaces.
The Linux artifact built on Debian bookworm targets glibc systems, not musl/Alpine.

## v8.0.45 acceptance record

Historical production TTL results are preserved in
[the old TTL acceptance record](claude-uniform-ttl-verification.md); those test
implementations and the feature have been removed from the current working tree.
`TestCPACompatibility` now also loads both historical TTL producer versions,
checks unchanged disk state and exact receipt replay, rejects new TTL writes,
preserves model filtering and ignores an invalid historical metadata path.

See [cpa-v8.0.45-verification.md](cpa-v8.0.45-verification.md) for the actual
2026-10-28 Windows/UCRT results and A-01–A-15 evidence. Go 1.26.10 is the minimum
for the pinned SDK. Docker/Linux, real Codex-client and live deployment/provider
checks were not rerun for this target; their historical results do not certify it.
The subsequent response-verification removal and its final regression results
are recorded separately in [response-verification-removal.md](response-verification-removal.md).
