# CPA Compatibility

This file is the source of truth for CPA versions supported by this repository. It
must be updated before any ABI-facing change.

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

The current development target is CPA v7.3.8 only, C ABI 1 / host schema 6,
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
official authenticated `/api-keys`, `/config`, and `/auth-files` management routes;
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
- Both platforms: `go vet`, unit/management tests, race tests and real CPA v7.2.143
  dynamic-loading fixture passed. The fixture uses synthetic keys and a loopback
  OpenAI-compatible upstream, including streaming cancellation and policy restart.
- Browser: Playwright with Microsoft Edge; 1440x1000 desktop and 390x844 mobile;
  policy publication, reload and rollback exercised against the real Windows CPA.
- CPA v6 and no-plugin builds are unsupported; upgrade CPA before installation.
- The v7.3.7 host accepts the plugin's schema 4 registration. New cross-priority
  scheduling is not enabled; the original highest-priority-tier contract remains.
- Plugin-store installations add host-managed `store` metadata to the normalized
  plugin configuration. The plugin accepts this node while continuing to reject
  unknown plugin-owned configuration fields.
- Real external provider requests, distributed deployments, Alpine/musl and
  other CPA revisions were not tested. No compatibility claim extends to them.

## Pinning procedure

1. Select a CPA release or commit used by the deployment environment.
2. Record the exact tag/commit, Go toolchain, plugin ABI version, hook names, metadata
   keys, and management route behavior.
3. Build a minimal hello-world ABI fixture against that revision.
4. Add fixture results and incompatibilities to this table.

Do not claim compatibility with a moving `main` branch. If CPA changes a hook payload,
host table, or ABI entry point, create a new compatibility row and keep the old adapter
until its support window ends.
