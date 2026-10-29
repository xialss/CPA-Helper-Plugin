# CPA Compatibility

This file is the source of truth for CPA versions supported by this repository. It
must be updated before any ABI-facing change.

## Release 0.1.12

Plugin 0.1.12 retains C ABI 1, the independently implemented RPC schema 7 and
Host-max negotiation without an upper-bound whitelist. The SDK and `cpa_version`
provenance remain v8.0.45. Policies from 0.1.0 through 0.1.11 and saved catalogs
from 0.1.11/0.1.10/0.1.9 with v8.0.45 or 0.1.8 with v8.0.32 remain readable;
new catalog publications require 0.1.12/v8.0.45. No startup state rewrite is added.
Release progress and final artifact evidence: [v0.1.12](v0.1.12-release.md).

### Running-version display

The management UI reads `X-CPA-VERSION` from successful existing management
responses to display the running CPA version in its model-catalog source row.
The official v8.0.45 and v8.0.47 management middleware sets this header from
`buildinfo.Version` (`internal/api/handlers/management/handler.go:325`). If no
response reports it, the UI explicitly shows "版本未报告". The catalog envelope's
`cpa_version` remains pinned data-contract provenance, independent of this display;
it must not be replaced with a running-version value. Contract, catalog status and
catalog revision remain in the API but are hidden in the runtime-status UI.

Validated on 2026-10-29 with a rebuilt embedded-resource DLL and the official
v8.0.45 and v8.0.47 Windows binaries in headless Edge. Both fixtures verify the
running-version display, unchanged `v8.0.45` catalog envelope, removed rows,
explicit missing-header display and restoration after the header returns.
JavaScript syntax and diff checks also pass. These initial UI results precede the
version bump; final-version and published-artifact checks are tracked in the
release record. A catalog with a saved revision but no usable producer explicitly
shows "来源不可用" and retains its error details.

### Remove Claude TTL

The user requested removal of all plugin TTL functionality on 2026-10-29.
This change is not part of the published v0.1.11 assets. It retains C ABI 1,
the independent RPC schema 7 and Host-max negotiation (minimum 7, no upper bound).
It removes the usage capability/callback, TTL request/selection state, header
mutation, configuration-file checks, execution metadata storage and management/UI
surfaces. The after-request callback becomes a no-op while both phases still omit
request bodies. Model-list filtering and policy/scheduler/lifecycle contracts stay.

Old plugin YAML is accepted with a removal warning and ignored. Valid old policy
TTL fields are retained only for stored-history/receipt compatibility, excluded
from compiled decisions and current-policy output; new nonempty TTL policy writes
fail explicitly. Startup never rewrites old state. Old TTL metadata is not read,
written or deleted. Independently installed CPA payload rules must be removed
from CPA configuration by its administrator; the plugin does not edit CPA files.
Scope, rollback and verification: [TTL removal](claude-ttl-removal.md).

## Published release 0.1.11

Plugin 0.1.11 ships schema negotiation using C ABI 1 and the independently
implemented plugin RPC schema 7. Official CPA v8.0.45 and v8.0.47 are the tested
Hosts; there is no exact CPA release or Host-schema upper-bound whitelist.
The SDK and `cpa_version` provenance stay v8.0.45. Policies from 0.1.0 through
0.1.10 and saved catalogs from 0.1.10/v8.0.45, 0.1.9/v8.0.45 and 0.1.8/v8.0.32
remain readable without a startup rewrite. New catalog publications use
0.1.11/v8.0.45. Back up state before upgrade; binary rollback restores matching
binaries and pre-upgrade state. Both platform builds and official artifact checks
passed; the downloaded Windows DLL passed CPA/TTL fixtures on both official Hosts
and Edge smoke on v8.0.47. Linux dynamic loading is untested. Publication and
artifact evidence are tracked in [the v0.1.11 release record](v0.1.11-release.md).

## Compatibility policy: negotiate the implemented contract

The plugin requires native C ABI 1 and RPC schema 7 or newer at the Host. A Host
schema is a maximum supported contract, not an exact version whitelist. The
plugin declares its own implemented `RPCSchemaVersion` (7), independently of the
SDK's latest `pluginabi.SchemaVersion`, and keeps using that contract when a Host
adds schema 8, 9 or later. Updating an SDK must not implicitly raise this minimum
or enable new hooks. Schema 7 is required by the request-body omission capability.

Pinned SDK revisions and verified Host releases are build/reproduction evidence;
`cpa_version` is the pinned data-contract provenance, not a runtime Host-version
gate. Additive schemas and unknown registration fields do not require a plugin
release. Actual incompatible native ABI or required callback/route semantics
remain compatibility boundaries and must fail explicitly rather than bypass policy.
Future-schema simulations and a newer-SDK build regression protect this rule;
they are not claims that an unknown future Host has passed end-to-end tests.
The same adapter passed its registration/removal tests built against the official
v8.0.47 SDK (latest schema 8) via an isolated modfile, including Host 7 acceptance
and schema-7 management output. The project's pinned SDK files were not changed.
The pre-release DLL with the independent schema constant passed the real CPA/TTL suites
on official v8.0.47 (57.473 seconds) and v8.0.45 (48.945 seconds), plus v8.0.47 Edge.

## Fix included in 0.1.11: host schema negotiation on CPA v8.0.47

On 2026-10-29 a user reported Plugin v0.1.10 failing on official CPA v8.0.47
(`90c447d3a8727d115aa7552ec93ce023736ed492`) with `CPA schema 7 is required`.
CPA v8.0.47 advertises host RPC schema 8, still uses native C ABI 1, and accepts
plugins declaring an older supported schema. The plugin incorrectly treated the
host's maximum schema as an exact required match.

The fix keeps the pinned v8.0.45 SDK and the plugin's declared schema 7, accepts
host maxima at least 7, and rejects older hosts with both required and received
schema in the error. It does not opt into schema-8 raw-body patch callbacks.
Target provenance (`cpa_version=v8.0.45`), policy/catalog envelopes, TTL payload
contract, body-omission flags and removed verification hooks remain unchanged.
This is negotiation of the pinned contract, not an unverified SDK migration or a
promise that every future CPA version is compatible. Official v8.0.47 and the
existing v8.0.45 binary both passed the three real-host CPA/TTL suites with the
fixed DLL (45.050 and 40.169 seconds respectively); v8.0.47 Edge smoke, fmt,
vet, all Go tests and race passed on Windows amd64. The published v0.1.10 DLL
reproduced the reported failure on the same v8.0.47 Host. The fix remained local
at that implementation stage; Linux dynamic loading was not tested. The user
subsequently authorized release 0.1.11; its publication and final-version results
are recorded separately above. Existing v0.1.10 assets remain unchanged.

Investigation, reproduction and final evidence: [schema negotiation fix](cpa-v8.0.47-schema-negotiation.md).

## Published baseline: Plugin 0.1.10 / CPA v8.0.45

Plugin 0.1.10 targets CPA v8.0.45
(`ad8bdd3f20bef44f0684b8a6096a216b2c76040d`), C ABI 1 / RPC schema 7,
with Go 1.26.10 or newer. It includes uniform Claude TTL and removes response
verification. Policies from 0.1.0 through 0.1.9 remain readable; saved catalogs from
0.1.9/v8.0.45 and 0.1.8/v8.0.32 retain their provenance and are not rewritten on
startup. New catalog publications require 0.1.10/v8.0.45. Back up state before
upgrading; rollback restores compatible binaries and the pre-upgrade state.
See [the release record](v0.1.10-release.md) for publication and artifact validation.
It is released as [v0.1.10](https://github.com/xialss/CPA-Helper-Plugin/releases/tag/v0.1.10)
from source `a8884145e881d43c49523b3e29a65a84104de309`. Both platform builds
passed CI; the downloaded Windows DLL passed all three real CPA/TTL fixtures
(40.469 seconds) and Edge smoke. Linux dynamic loading was not tested.

The 0.1.10 implementation removes response model verification. It still targets
CPA v8.0.45, C ABI 1 / RPC schema 7. It no longer registers raw-response or stream
interception hooks, and omits request bodies both before and after authentication.
Model-list filtering keeps the ordinary response hook. Legacy
`response_model_mismatch` YAML is accepted only to allow upgrades, ignored with
an explicit warning, and cannot enable the removed feature. Its management route,
capabilities, health fields and UI are removed. See
[the removal record](response-verification-removal.md) for scope and validation.
This is not a change to the published v0.1.9 binary.

Removal verification passed on 2026-10-28 with Windows amd64 / Go 1.26.10 /
UCRT GCC 16.2.0: fmt, vet, all Go tests, race, final library build, real CPA
compatibility and both production TTL suites, 13 directory tests and Edge smoke.
Legacy enabled/reject YAML cannot restore verification. Responses text arrives
before upstream completion; cancellation, genuine truncation, model-list filtering
and policy enforcement pass. VPS memory profiling, Linux/Docker runtime, a real
Codex client and external-provider/Helper billing were not tested for this change.

The 0.1.10 uniform Claude TTL implementation uses this same pinned CPA /
C ABI 1 / RPC schema 7. Its public scheduler selection ID, request headers,
payload rules and usage execution ID passed local Windows real-host fixtures.
See [the TTL verification](claude-uniform-ttl-verification.md) for strict setup
prerequisites and untested boundaries. This does not change the published 0.1.9
binary or claim Helper billing/provider acceptance has been verified.

The uniform TTL rules also require CPA v8.0.45 payload model `match` conditions
for existing top-level automatic `cache_control`. The root TTL is normalized only
when `cache_control.type` is `ephemeral`; an absent root control stays absent.

The opt-in Claude TTL feasibility probe under `integration/testdata/ttlprobe`
targets this same CPA release, C ABI 1 and RPC schema 7. It is a diagnostic-only
plugin, never a supported production artifact or an additional billing source.
See [claude-ttl-feasibility.md](claude-ttl-feasibility.md) for its scope and results.

## Published baseline: Plugin 0.1.9 / CPA v8.0.45

Plugin 0.1.9 targets CPA v8.0.45
(`ad8bdd3f20bef44f0684b8a6096a216b2c76040d`), C ABI 1 / RPC schema 7,
with Go 1.26.10 or newer. Verified locally on Windows amd64 on 2026-10-28;
released as [v0.1.9](https://github.com/xialss/CPA-Helper-Plugin/releases/tag/v0.1.9)
from source `1e98b18461898f200642677931ce44df8f5e534d`. The implementation
and acceptance evidence are tracked in [tasks.md](tasks.md), with requirements in
[cpa-v8.0.45-prd.md](cpa-v8.0.45-prd.md).

The published 0.1.9 binary requires schema 7, omits request bodies only after authentication,
and uses `host.auth.models` for actual credential/model associations. Valid
0.1.8/v8.0.32 catalogs must remain readable without an automatic disk rewrite;
an explicit synchronization writes the next revision for the new target. Unknown
or corrupt snapshots remain errors. Policy v1 and credential fingerprints remain
unchanged. Back up the complete state directory before upgrade. Rollback requires
stopping CPA and restoring matching CPA/plugin binaries and the pre-upgrade state
backup; old binaries are not promised to accept newly written state.

The management-only `/credential-models` calls `host.auth.models`, hashes each
actual scheduler ID and returns its registered public models. No per-credential
model query or scheduler call is needed. The configuration ID bridge remains only
to supply source/status metadata missing from Host inventories, with exact
fingerprint/provider/index validation. Shared auth indexes remain distinct.
Fully validated Host-only entries with no registered models are excluded as
non-owners, with a visible UI count; no source/status is inferred. Known metadata
entries with empty models stay in the catalog, and unknown nonempty owners or
other malformed/mismatched entries still fail the full synchronization. This
handles CPA's disabled runtime-only AIStudio inventory omission without dropping
any model owner. The complete normalized Host set, including excluded entries,
is checked on both reads.
Double-read validation covers observed model-set changes as well as identity and
configuration, but CPA does not provide a global atomic registry revision.
The explicit refresh button POSTs `/v8/management/models/refresh` once before
synchronizing; automatic and ordinary sync do not refresh model sources.

Go 1.26.10 / UCRT GCC 16.2.0 checks passed: fmt, vet, all Go tests, race,
dynamic-library build, the official CPA compatibility/management fixture and Edge
browser smoke, including actual Host callbacks and the local-model refresh route.
The initial implementation passed 10 directory tests; post-review regression
results are recorded in [cpa-v8.0.45-review.md](cpa-v8.0.45-review.md).
The official Windows archive SHA-256 matched release
checksums: `b049739ca8cd1885c59bdf57de6adf12452e9ab9907f50117906c11fadffff1b`.
Real fixtures cover files with relative auth-dir, config/keyless/disabled entries,
shared indexes, aliases/prefixes/exclusions, legacy catalog upgrade/receipts/restart,
refresh failure stages/draft retention and existing routing/lifecycle behavior.
CPA's current Chat handler terminates and cancels execution on a plugin error
chunk; it does not forward a subsequent finish chunk or append a false truncation.
Genuine truncation without plugin rejection still reports an error.

The release workflow passed source validation, Linux amd64 and Windows amd64
builds, and publication. Both official archives were built with Go 1.26.10;
their embedded source revision and SDK version match the tag. All three downloaded
assets matched GitHub SHA-256 digests; both ZIPs matched `checksums.txt` and contain
exactly their root-level library. The downloaded Windows DLL passed the real CPA
compatibility/management fixture and complete Edge smoke again.

See [cpa-v8.0.45-verification.md](cpa-v8.0.45-verification.md) for A-01–A-15,
release artifact evidence and observed toolchain failures. Linux/Docker CPA runtime
and actual Codex-client smokes were not rerun; no live DSH profile, deployed service or external provider
credential was used. Header-identity and model-list HTTP-status limitations remain.

## Historical target: CPA v8.0.32

Plugin 0.1.8 targets v8.0.32
(`911f49cbd49f4fe2516979754a7488a3fd14fe64`), C ABI 1 / RPC schema 6.
The scheduler, host tables and model-list response
contracts are unchanged from v8.0.29. The host still omits the authenticated
principal and model-to-credential associations from model-list callbacks, and
response interception cannot change their HTTP status.

Credential-aware catalog filtering uses an explicit local snapshot populated
through the official `/v8/management/config`, `/v8/management/credentials`, and
`/v8/management/credentials/models?name=<authID>` APIs. `name` is a scheduler
auth ID or auth filename, not `auth_index`. Registered public IDs preserve CPA
aliases and prefixes. The existing pinned configuration-to-scheduler-ID bridge
is retained; no CPA internal package is imported.

The versioned plugin `/v1/model-catalog` management transaction publishes the
complete validated snapshot atomically, with revision preconditions and durable
idempotency. `model-catalog.json` is separate from the unchanged v1 policy state.
No raw keys, auth IDs, labels or management credentials enter that file. Failed
synchronization keeps the last valid directory; missing/incomplete directories
produce explicit list errors for callers with credential restrictions. Catalog
reads do not run scheduling, reserve concurrency, or contact an upstream.

Synchronization runs when the plugin panel opens and on its directory-sync
button. Changes to CPA credentials, registered models, aliases or prefixes need
another synchronization. Policy changes apply immediately to the saved directory.
Header identity limitations and the existing unfiltered-identity-unavailable
behavior remain. List errors still use an HTTP 200 body/header replacement;
hidden model details use CPA's existing 404 handling.

Rollback restores the previous plugin/CPA binaries and a policy backup accepted
by that plugin. Older plugins ignore the separate catalog file. Newly written
0.1.8 policies need the corresponding previous-version backup when rolling back.

Verified on 2026-10-17 with Go 1.26.4 / Windows amd64 / UCRT GCC 16.2.0:
formatting, vet, all Go unit/management tests, race tests, the real CPA v8.0.32
compatibility fixture, directory tests and the Edge browser smoke passed.
The official Windows archive SHA-256 was checked before use:
`df364b1f2dc9fe357f6a481bfb8a4066c3e5f5ef4ad7abfe45e59b4cec547c18`.
The real fixtures verify credential-only filtering, hidden detail 404, shared
owners, aliases/prefixes, catalog synchronization/failure/restart, and the
existing weight, generation, stream, configuration and lifecycle contracts.
Unit fixtures cover file auth IDs, disabled owners and corrupt/uncertain storage.
Linux/Docker, a live DSH profile and external provider calls were not tested.
The existing header-identity and HTTP-status restrictions remain. No deployed
service or real credential was modified.

## Historical target: CPA v8.0.29

Plugin 0.1.7 targets v8.0.29 (`5fc580b1d72d87a7bcfe5cfb6f479c1aea5625b3`),
C ABI 1 / RPC schema 6, unchanged from v8.0.17. The pinned SDK adds
`ConfigField.Default`; registration now describes the same defaults used when
`state_dir`, `model_list_filter_enabled`, or `response_model_mismatch` is omitted.
The host exposes these values through `/v8/management/plugins`. The new
`UsageRecord.IsNativeKey` and `AccessProvider` fields do not affect this plugin,
which does not register a usage callback.

CPA commit `67465884` detaches disabled or unselected plugins when applying
configuration, including every plugin when `plugins.enabled` is false. Keep both
`plugins.enabled: true` and `plugins.configs.cpa-helper-plugin.enabled: true` in
persisted configuration. A disabled plugin becomes unregistered and loses its
management/resource routes; re-enabling it must reload the persisted policy.
Final shutdown now clears request/concurrency and scheduler state: CPA no longer
delivers completion events to a detached plugin, and Windows can reuse its DLL
when it is re-enabled. Temporary quiesce still retains accounting for replacement
rollback. The fixture finishes an admitted stream while disabled and checks that
re-enabling restores admission, alongside configuration defaults across the
dynamic registration and management API boundary.

CPA's `server.max-request-body-bytes` limit can reject large policy/config writes
with HTTP 413 before they reach the plugin. This host setting is independent of
the plugin's Responses buffer limits.
No policy/config migration is required; 0.1.6 snapshots remain accepted. Rollback
restores the previous binary and a policy backup accepted by that binary. The
configuration defaults are metadata only and require no rollback edits.

Verified on 2026-10-16 with Go 1.26.4 / Windows amd64 / UCRT GCC 16.2.0:
formatting, vet, unit tests, race tests, a newly built dynamic library against the
official v8.0.29 binary, management integration, directory tests and Edge browser
smoke passed. The official archive checksum was checked before use. The fixture
verified configuration defaults and per-plugin/global disable/re-enable recovery,
alongside the existing response, routing, cancellation, restart and rollback
contracts. Race linking required a compiler directory without spaces; GCC expands
8.3 aliases back to a path containing spaces. Linux/Docker verification was not
run because the local Docker daemon was unavailable. External providers and
terminal HTTP flush failure injection were not tested. No deployed service was
modified. The v8.0.17 record below is historical.

## Version 0.1.6: historical v8.0.17 verification

Plugin 0.1.6 targets v8.0.17 (`57bde35179ecbdca176ca8923d39cc18805774f2`),
C ABI 1 / RPC schema 6, unchanged from v8.0.12.
Chat rejection retains the explicit verification error and only real upstream
finish reasons, with empty deltas. It never invents a successful terminal marker;
genuinely truncated streams still receive the host truncation error. This avoids
v8.0.15+'s false truncation when suppressed chunks contain the real finish reason.
No policy/config migration is required; 0.1.5 snapshots remain accepted.
Before downgrading, restore a policy backup accepted by the previous binary.
Rollback restores the previous binary
and reintroduces false truncation reporting. The ABI still cannot abort Chat
execution; subsequent content remains suppressed until upstream completion.

Verified on 2026-10-07 with Go 1.26.4 / Windows amd64 / UCRT GCC 16.2.0:
formatting, vet, unit tests, race tests, official v8.0.17 dynamic-loading and
management integration, and Edge UI smoke passed. The release archive checksum
was checked before extraction. Regression cases cover real Chat finish reasons
after rejection (including content on the terminal chunk), genuine truncation,
Responses clean EOF without [DONE] versus truncated EOF, model detail visibility,
and lifecycle accounting. Existing cancellation, restart, credential selection,
null keyless lists and prototype-named header contracts also passed.
Terminal HTTP flush failure injection, Linux/Docker and external providers were
not tested. No deployed service was modified.

## Version 0.1.5: historical v8.0.12 verification

Plugin 0.1.5 targets v8.0.12 (`2044a01f422998de79a5da8015141b878886534d`),
C ABI 1 / RPC schema 6. Host configuration and credential discovery use the v8
management API. The directory exposes host-provided `auth_index` for diagnostics;
policy references still hash the scheduler credential ID. v8.0.12 omits configured
credentials from `/credentials` and does not expose indexes in scheduler hooks,
so a pinned configuration-to-ID bridge remains necessary. Indexes are not unique
across all configured credentials and must not be used to deduplicate them.
There is no v0 host API fallback. Plugin-owned management/resource routes retain their v0 namespace,
as required by CPA's dynamic plugin route registrar. No policy migration is needed.
Rollback restores the previous plugin binary and a backup of CPA configuration
if v8 writes migrated its layout. The `responses_only_stream` option and its
protocol rejection gate are removed. Remove that field from existing CPA config
before loading this build; unknown fields remain explicit configuration errors.
Responses buffering defaults are 2 MiB per request and 8 MiB total. The UI uses
MiB, while the API/YAML retain byte units. Other streaming protocols are verified
per chunk and cannot retract already delivered content.
Verified on 2026-10-03 with Go 1.26.4 and WinLibs UCRT GCC 16.2.0 on Windows
amd64: `go fmt`, `go vet`, unit tests, race tests, and the real CPA v8.0.12
dynamic-loading/management fixture passed. The official CPA binary checksum was
verified against its release checksums. The directory tests and Edge browser smoke
passed, including real credential-restricted routing, v8 configuration replacement,
mapping deletion, sibling-setting preservation, MiB conversion and reload.
Linux/Docker and external providers were not tested for this revision; the local
Docker daemon was unavailable. No remote deployment was modified.

Directory regression contracts: own header names such as `__proto__` participate
in the scheduler-ID hash, including inherited group headers. OpenAI-compatible
`keys: null` is read as a keyless list, retaining the group `auth_index`; malformed
non-list values still fail sync. Real v8.0.12 browser tests verify null discovery
and both allow/deny routing for a Claude credential with a `__proto__` header.
Previously saved references generated without that header must be reselected and
saved in the panel; the policy schema is unchanged. Rolling back this adapter
reintroduces the incorrect reference and null-list failure.

## Version 0.1.4: response model verification (historical v8.0.3 verification)

Only the latest official CPA release is targeted; old adapters are not retained.
Each build pins an exact release for reproducibility. Historical rows below are
verification records, not a promise of support. The target of this record was
v8.0.3 (acdace93), C ABI 1 / host and plugin RPC schema 6. Registration rejects
other schemas. The handshake does not expose the host release number, so this
is not an exact host-version runtime check; cpa_version reports the build target.
Response verification compares the client model with upstream-declared identity
and must not confuse translated model fields with raw upstream identity. Strict
mode rejects unsupported non-Responses streaming requests before execution;
request-before model mismatch rejection is not implemented. Configuration is
host-managed and disabled by default.
Rollback requires restoring the prior library and removing the new
`response_model_mismatch` configuration node while CPA is stopped.
If a 0.1.4 policy snapshot was saved, also restore a policy backup accepted by
the prior binary.

New dependency: response.normalize_before provides raw upstream Body and
OriginalRequest before built-in translation, without RequestID. A unique live
original-body SHA-256 associates it with the request hook. Identical overlapping
bodies remain explicitly unverifiable. No request bodies or hashes are logged
or persisted. response.intercept_after replaces non-stream responses;
response.intercept_stream_chunk uses JSON for OpenAI Chat and framed SSE for
Claude, Responses and Gemini, then DropChunk suppresses subsequent output.
Response hooks cannot override HTTP status or terminate upstream execution.
request.complete releases verification state. Policy v1 accepts 0.1.0–0.1.4.

Response-verification diagnostics use the official `host.log` callback with
`level`, `message`, and `fields` (plugin_id/request_id). v8.0.3's formatter only
prints selected fields, so the quoted decision/model details are in the message.
The fixture verifies delivery through CPA's authenticated `/v0/management/logs`.
No policy/config migration is needed; restore the prior library to roll back logs.

Responses stream rejection uses an explicit `response.failed` terminal event,
error status 403 and `invalid_prompt` as the Codex-compatible non-retryable wire
code. `verification_code` and the message retain the actual verification reason.
Codex treats unknown response.failed codes as retryable; retryable=false alone
does not change its classification. CPA v8.0.3 recognizes error.status and
cancels the Responses execution context on terminal errors. This dependency
requires a real streaming cancellation fixture and Codex CLI retry acceptance.

Responses reject mode now withholds all successful payloads until a complete
terminal response event and the model decision. This prevents early text/tool
events from executing before a late model declaration. UnknownAction is applied
at the terminal event, not to intermediate missing-model chunks. Raw-body
observation and DropChunk remain the only ABI dependencies. Buffered SSE bytes
stay in request-local memory and are discarded on rejection/cancellation; memory
use is proportional to the pending response. No config migration is required.
CPA's native Codex translator emits complete data lines without delimiters, and
its Chat translator emits event/data pairs without trailing delimiters. Captured
unit fixtures normalize both forms; fragmented standard SSE remains buffered.
Real Codex 0.155.1 checks cover late model declarations and model changes after
complete tool items: no text/tool leakage, one upstream request, cancellation
and no reconnection. The previous deployed library reproduces the tool leak.
Strict stream mode rejects non-Responses streaming requests before execution,
because the v8.0.3 ABI has no general stream-abort callback for Chat, Claude,
or Gemini. Responses buffering is bounded per request by `max_buffer_bytes`
(default 16 MiB, configurable up to 256 MiB); exceeding it fails closed.

Verification on 2026-09-28: official v8.0.3 Linux amd64 release binary,
Go 1.26.5 / Debian bookworm, in isolated Docker fixtures. Unit/management tests,
vet, race and dynamic-loading tests passed. Model mismatch rejection passed
OpenAI Chat non-stream/stream, Responses non-stream/stream, and Claude/Gemini
streaming clients translated from a synthetic OpenAI upstream. Explicit mappings,
unknown rejection and original admission/cancellation/restart/rollback passed.
Edge browser tests passed configuration validation, save, reload, CPA restart
persistence, real error/header delivery and desktop/mobile layouts.
Native external provider connections, arbitrary other plugins and WebSocket
interception are not certified. The existing service was upgraded to the v8.0.3
release image after plugin, configuration and policy-state backup.

## Version 0.1.2 model-list filtering

Client request errors now use Chinese messages. Disabled policy keys return
401/api_key_disabled instead of 403/model_forbidden. Only header keys whose
fingerprint matches the host caller_scope are displayed, masked to six leading
and four trailing characters (keys of ten or fewer characters are fully masked).
When the original key is unavailable, the message omits it. Model denial messages
identify matching key/group deny rules or the merged allow-list sources.
No policy format migration is required. Model-list status limitations remain.

Version 0.1.2 reports CPA v7.3.8 and adds the host-managed boolean configuration
`model_list_filter_enabled` (default true). Reconfiguration applies it immediately;
false bypasses only model-list response processing. Policy v1 snapshots from
0.1.0/0.1.1 remain accepted. Before binary downgrade remove the new config field;
restore a matching policy backup if a newer producer version has been saved.

The historical 0.1.2 development target was CPA v7.3.8, C ABI 1 / host schema 6,
plugin schema 4. Older releases below are historical verification records.
This release adds `response.intercept_after` using v7.3.8's
`WriteModelListResponse`: empty Model/RequestedModel and request bodies,
HTTP 200, original request headers, and nil Metadata. It filters OpenAI data/id,
Claude data/id (including cloaked IDs), Gemini models/name and Codex models/slug.
Only model allow/deny rules and key enablement apply to listing; credential
availability and scheduler priority are still evaluated on execution.

Use a single header credential (Authorization, X-Api-Key or X-Goog-Api-Key) for
per-key filtering. The hook does not expose the URL or authenticated principal.
Missing or distinct header credentials preserve the original catalog after CPA
authentication, with an observable X-CPA-Helper-Model-List response header value
`unfiltered-identity-unavailable` and a structured log. This is an intentional
visibility relaxation for trusted users, not an authentication bypass. A wrong
header credential combined with a valid query credential can still select the
wrong display policy. Unavailable policy and malformed catalogs continue to return
an explicit JSON error and X-CPA-Helper-Error; CPA still sends HTTP 200 because
this hook cannot override status. Errors must be returned as successful RPC
body replacements, since CPA ignores interceptor RPC failures.
Header identity is an inference, not an authenticated-principal guarantee.
The host can also skip failed/fused plugins; listing is not a security boundary.
Generation admission remains authoritative. No snapshot migration is needed;
restore the previous library to roll back this release.

Verification on 2026-09-19: Linux amd64, Go 1.26.5 / Debian bookworm GCC 12.2,
using the unmodified CPA v7.3.8 (`c93978c`) binary copied from the user's running
Docker container in a separate test container. Vet, unit/management tests, race
tests and the real CPA fixture passed. The fixture verifies filtering through
Authorization, X-Api-Key and X-Goog-Api-Key; unconfigured callers keep the full
catalog. The initial trial returned explicit errors for query-only and distinct
header identities; the relaxed revision preserves the original catalog instead.
Original execution, concurrency, cancellation, restart and rollback checks also
passed without changing the running service's keys or policy snapshot.

The relaxed revision subsequently passed the same Linux checks and real CPA
fixture, including query-key/query-auth-token catalogs, conflicting headers,
anonymous/invalid-key 401 and restricted-generation 403. It was deployed to the
existing v7.3.8 Docker service after library/state backup. Configuration and
policy hashes remained unchanged; the service loaded the replacement plugin.

| Plugin release | CPA version | CPA plugin ABI | Status | Notes |
| --- | --- | --- | --- | --- |
| 0.1.12 | v8.0.45 and v8.0.47; pinned SDK v8.0.45 | C ABI 1 / host schema at least 7, plugin schema 7 | published; both platform builds/artifacts checked; official Windows DLL and Edge passed on both Hosts; Linux loading untested | Removes TTL; retains 0.1.11 state/receipt compatibility; displays the running CPA version |
| 0.1.11 | v8.0.45 and v8.0.47 (`90c447d3a8727d115aa7552ec93ce023736ed492`); pinned SDK v8.0.45 | C ABI 1 / host schema at least 7, plugin schema 7 | published; both platform builds/artifacts checked; official DLL CPA/TTL passed on both Hosts and Edge on v8.0.47; Linux loading untested | Accepts compatible newer-host schema maxima while retaining body omission and the pinned plugin contract |
| 0.1.10 | v8.0.45 (`ad8bdd3f20bef44f0684b8a6096a216b2c76040d`) | C ABI 1 / host and plugin schema 7 | released; official Windows DLL verified; Linux CI tests/build passed | Removes response verification; optional uniform Claude TTL; reads saved 0.1.9 and 0.1.8 catalogs |
| 0.1.9 | v8.0.45 (`ad8bdd3f20bef44f0684b8a6096a216b2c76040d`) | C ABI 1 / host and plugin schema 7 | released; official Windows DLL verified; Linux CI tests/build passed | Authoritative Host model associations, explicit refresh, legacy catalog read compatibility and runtime-only empty-owner exclusion |
| 0.1.8 | v8.0.32 (`911f49cbd49f4fe2516979754a7488a3fd14fe64`) | C ABI 1 / host and plugin schema 6 | verified on Windows amd64 | Credential-aware model catalogs via official management API snapshots |
| 0.1.7 | v8.0.29 (`5fc580b1d72d87a7bcfe5cfb6f479c1aea5625b3`) | C ABI 1 / host and plugin schema 6 | verified on Windows amd64 | Configuration defaults and disable/re-enable lifecycle contracts |
| 0.1.6 | v8.0.17 (`57bde35179ecbdca176ca8923d39cc18805774f2`) | C ABI 1 / host and plugin schema 6 | verified on Windows amd64 | Preserve real Chat termination after rejection; Responses EOF and model detail contracts |
| 0.1.5 | v8.0.12 (`2044a01f422998de79a5da8015141b878886534d`) | C ABI 1 / host and plugin schema 6 | verified on Windows amd64 | v8 management, indexed credential directory with pinned scheduler-ID bridge, per-chunk non-Responses verification |
| 0.1.4 | v8.0.3 (`acdace936fa7df2905500c7f5e0a97d683138dea`) | C ABI 1 / host and plugin schema 6 | historical; verified on Linux amd64 | Configurable raw upstream response model verification and HTTP/SSE interception |
| 0.1.3 | v7.3.12 (`2eb8dd11`) | C ABI 1 / host and plugin schema 6 | historical verification record | Initial response model verification and HTTP/SSE interception |
| 0.1.2 | v7.3.8 (`c93978c`) | C ABI 1 / host schema 6, plugin schema 4 | verified on Linux amd64 | Model-list filtering, host-managed toggle and Chinese policy errors; policy schema accepts 0.1.0–0.1.2 snapshots |
| 0.1.0 | v7.2.143 (`4b5f1eab25fca4b3815369a826e958e7c070a69e`) | C ABI 1 / RPC schema 4 | verified on Linux and Windows amd64 | Go 1.26.0 minimum; CGO required |
| 0.1.0 | v7.3.7 (`b773607e3e7756dc6020a291825e4eb08899595a`) | C ABI 1 / host schema 6, plugin schema 4 | verified on Linux amd64 | Fixture passed with the binary copied from the running Docker container |
| 0.1.1 | v7.3.7 (`b773607e3e7756dc6020a291825e4eb08899595a`) | C ABI 1 / host schema 6, plugin schema 4 | verified on Linux amd64 | Store-installed configuration accepted; 0.1.0 policy snapshots remain compatible |

The plugin imports only the public `sdk/pluginapi` and `sdk/pluginabi` packages.
The reference behavior is cpa-plugin-key-billing commit
`fd8fe60be318987fd07b21b7f792564e9bee9c74` (MIT).
Contract dependencies: `plugin.register`, `plugin.reconfigure`, `plugin.quiesce`,
`plugin.shutdown`, `request.intercept_before`, `request.intercept_after`,
`request.complete`, `scheduler.pick`, `management.register`, `management.handle`,
`host.auth.list`; metadata `caller_scope`, `requested_model`, `generate`, `source`.
Only CPA's supplied candidate tier is considered. Subset weighted selection is
required because this ABI cannot delegate a filtered candidate list. There is no
cross-priority fallback. Usage observation and billing are not registered.

The UI reuses the same-origin CPA panel's `cli-proxy-auth` / `managementKey`
storage contract, including `enc::v1::` encoding. Credentials are rendered from
official authenticated `/v8/management/config` and `/v8/management/credentials` routes;
policy snapshots still contain only stable identifiers and user-entered notes.
The deployed v7.3.7 panel storage format was checked against its served asset.
The browser fixture covers automatic session reuse and credential display.

Optional group `note` is now supported as display-only text; older snapshots without
it remain valid. Older plugin binaries reject snapshots containing `note`; before
downgrading, restore the pre-upgrade state backup while CPA is stopped. The policy
contract version remains v1 and routing semantics are unchanged.

State format v1 is new; no billing-plugin database import is supported. Back up the
whole state directory before upgrades. Restore it only while CPA is stopped.
Policy rollback republishes the retained policy at a new revision; it never rewinds
the revision counter. Do not downgrade a state format without a matching backup.

## Verification (2026-09-18)

Review fixes preserve ABI 1/schema 4 and state format v1. The pinned v7.3.7
`rollbackReplacement` reconfigures the quiesced old instance; successful
reconfiguration resumes admission without resetting live request accounting.
Policy field names require exact JSON tag spelling. Linux persistence synchronizes
the containing directory after file replacement and the state directory's parent
after creation. An uncertain replacement fails closed until the store is reopened.
No migration is required; backups remain the binary rollback procedure.

- Windows amd64: Go 1.26.4, WinLibs UCRT GCC 16.2.0 / MinGW-w64 14.0.0.
- Linux amd64: Go 1.26.5, Debian bookworm container, glibc dynamic library.
- Linux amd64: `go vet`, unit/management tests, race tests and the real CPA v8.0.3
  dynamic-loading fixture passed. The fixture uses synthetic keys and a loopback
  OpenAI-compatible upstream, including streaming cancellation and policy restart.
- Windows amd64: native unit tests passed. The local MinGW 8.1 race executable
  could not start because its runtime lacked an entry point; use the current UCRT
  toolchain described in `testing.md` before treating a Windows race run as valid.
- Browser: Playwright with Microsoft Edge; 1440x1000 desktop and 390x844 mobile;
  policy publication, reload and rollback exercised against the real Windows CPA.
- CPA v6 and no-plugin builds are unsupported; upgrade CPA before installation.
- The v8.0.3 host accepts the plugin's schema 6 registration. New cross-priority
  scheduling is not enabled; the original highest-priority-tier contract remains.
- Plugin-store installations add host-managed `store` metadata to the normalized
  plugin configuration. The plugin accepts this node while continuing to reject
  unknown plugin-owned configuration fields.
- Real external provider requests, distributed deployments, Alpine/musl and
  other CPA revisions were not tested. No compatibility claim extends to them.

## Pinning procedure

1. Select the latest official CPA release and pin its exact tag/commit.
2. Record the exact tag/commit, Go toolchain, plugin ABI version, hook names, metadata
   keys, and management route behavior.
3. Build a minimal hello-world ABI fixture against that revision.
4. Add fixture results and incompatibilities to this table.

Do not claim compatibility with a moving `main` branch. When CPA changes a hook
payload, host table or ABI entry point, update the single current adapter and
fixture before releasing. Older versions remain historical evidence only.
