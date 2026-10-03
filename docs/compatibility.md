# CPA Compatibility

This file is the source of truth for CPA versions supported by this repository. It
must be updated before any ABI-facing change.

## Current target: CPA v8.0.12

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
