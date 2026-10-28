# Management Contract

This repository implements only the CPA plugin. An external administrator may use
these endpoints to configure it, but no administrator-specific code, database, UI, or
runtime dependency belongs here.

CPA's official plugin management surface remains responsible for plugin registration,
lifecycle, and resource routing. The endpoints below are the plugin's generic policy
management surface.

## Endpoints

Model-list filtering is controlled through CPA's official plugin configuration:
`PATCH /v8/management/config/plugins/configs/cpa-helper-plugin` with
`{"model_list_filter_enabled":false}` disables it; true enables it (the default
when omitted). This host-managed setting persists across restarts and does not
modify the policy snapshot or disable generation enforcement. `/capabilities`
reports its current value, target CPA v8.0.45, plugin version 0.1.9, C ABI 1 and RPC schema 7.
`GET /v8/management/plugins` exposes each configuration field's `default`,
including the default state directory, enabled model-list filtering, and the
complete disabled-by-default response-verification settings.

Response verification uses `PUT /v8/management/config/plugins/configs/cpa-helper-plugin/response_model_mismatch`
with the complete verification object. This replaces cleared mappings while
preserving sibling plugin settings; PATCH would recursively retain old mappings.
The plugin panel exposes all its fields.
`POST /v1/response-model/validate` accepts this object, rejects unknown fields and
invalid enum/mapping values with 400, and returns normalized defaults without
mutating state. The panel validates, PUTs CPA, then polls capabilities until
the requested configuration is active (a save response alone is insufficient).
Failed reconfiguration leaves all prior plugin settings active. In-flight requests
retain their starting settings. See response-model-mismatch.md for field defaults,
response error shapes, raw-response association limits and rollback.

The v1 prefix is `/v0/management/plugins/cpa-helper-plugin/v1`. CPA's management
middleware authenticates every request using its management key. The plugin does
not open an additional listener or accept downstream API keys for administration.

| Method | Resource | Purpose |
| --- | --- | --- |
| `GET` | `/health` | plugin status, active revision, policy age, contract version, response_model_tracked_requests (all tracked executions, including disabled/exempt checks) |
| `GET` | `/policy` | complete policy with hashed identifiers; ETag is the revision |
| `PUT` | `/policy` | validate and atomically activate a complete snapshot |
| `POST` | `/policy/rollback` | activate a retained revision |
| `GET` | `/capabilities` | supported modules, contract and ABI versions |
| `GET` | `/directory` | host credential inventory plus observed configured credentials, with fingerprints only |
| `GET` | `/credential-models` | complete Host-registered credential/model associations, fingerprinted actual IDs and no scheduling |
| `GET` | `/model-catalog` | saved model associations and health; ETag is the independent catalog revision |
| `PUT` | `/model-catalog` | validate and atomically persist a complete model directory |
| `POST` | `/response-model/validate` | validate response-verification settings without saving |

Mutations require `If-Match: "<current revision>"` and `Idempotency-Key`.
`PUT /policy` supplies the next revision. `POST /policy/rollback` supplies
`{"policy_revision": <retained revision>}` and republishes its content at the next
revision. Both return `{"policy_revision": <accepted revision>}`. Durable receipts
make exact retries idempotent across restarts; key reuse with a different operation
returns 409. A retry may return an earlier accepted revision: always read health.

Missing preconditions return 428; invalid policy returns 400; stale revisions or
conflicting idempotency keys return 409; persistence failure returns 500 without
replacing the live snapshot; unavailable policy returns 503. Directory callback
failure is an explicit 502, not an empty successful directory.

## Snapshot envelope

```json
{
  "contract_version": "v1",
  "plugin_version": "0.1.9",
  "policy_revision": 2,
  "generated_at": "2026-09-18T00:00:00Z",
  "groups": [],
  "keys": []
}
```

The authoritative shape is `policy.schema.json`; unknown JSON fields are rejected.
Plugin `0.1.9` accepts snapshots produced by `0.1.0` through `0.1.8`;
all ten supported producer versions are enumerated in the schema. The policy contract remains
`v1`, and existing policy state does not require migration. Binary rollback must restore
the matching pre-upgrade state backup; do not rewrite producer metadata to bypass compatibility.
The complete `PUT /policy` operation is the external administration interface for
every control-panel setting. `groups[].id`, `groups[].name`, optional
`groups[].note`, `groups[].rule`, `keys[].id`, `keys[].label`, `keys[].enabled`,
`keys[].group_ids`, `keys[].max_concurrency`, and `keys[].rule` map directly to the
visible editor fields. `GET /capabilities.policy_fields` exposes this mapping for
CPA-Helper adapters. The model-list filter toggle is host-managed plugin
configuration and therefore intentionally remains outside the policy snapshot.
A client should read the
snapshot, modify the desired fields, preserve all others, then submit the complete
snapshot with `If-Match` and `Idempotency-Key`.
`groups` are reusable named route rules; each key binds zero or more `group_ids`
and an optional direct `rule`. `max_concurrency: 0` means unlimited per CPA instance.
`enabled: false` explicitly denies the key. An authenticated unconfigured key is
unrestricted by the plugin, provided a valid snapshot is loaded.

Key `id` is CPA's lowercase hexadecimal SHA-256 of
`"cli-proxy-api:caller-scope:v1\0" + trimmedRawKey`. Credential references are
`"sha256:" + hex(SHA-256("cpa-key-billing:credential:v1\0" + trimmedAuthID))`.
Raw keys are never persisted. The browser reads official CPA configuration only
in memory, computes identifiers, and sends only hashed selections to this API.

Rule fields: `models`, `denied_models`, `credential_ids`, `denied_credential_ids`,
`credential_providers`, `denied_credential_providers`. Category selectors contain
`source` (`auth-files` or `ai-providers`) and lowercase `provider`.
Within one rule, conflicting or duplicate selections are invalid. Across rules,
allow and deny lists union separately and deny wins. Empty allowlists impose no
additional restriction. Model matching is case-insensitive, uses the requested
alias, and removes CPA's trailing thinking suffix; `auto` uses the execution model.
Missing credential category metadata cannot bypass a potentially matching deny.

## Browser resources

`/v0/resource/plugins/cpa-helper-plugin/ui` and its sibling JS/CSS/logo resources
are public static assets. They contain no policy or credentials. The page requires
a CPA management key to fetch data, obtained from the same-origin CPA panel's
existing saved session. The plugin does not store another copy or provide its own
login form. If CPA has not saved the session (for example, Remember Password is
disabled), its current panel does not expose an in-memory session bridge; return
to CPA to establish a saved session. Missing sessions produce an explicit error.
Use HTTPS when accessing CPA over a network. No external CDN is needed at runtime.
The UI reads CPA `/v8/management/config` (downstream keys at `access.api-keys`,
upstream groups at `api-keys`), `/v8/management/credentials`, and the plugin's
`/credential-models`. The latter invokes `host.auth.models` with the management
callback context and returns `{"credentials":[{"ref":"sha256:...","auth_index":"...","provider":"...","models":["public/alias"]}]}`.
The fingerprint is computed from the actual Host ID, which is never returned.
Missing, malformed or duplicate IDs/model arrays reject the entire read with
502 `host_unavailable`; an explicit empty Host array is valid. Shared indexes are
retained. This read does not schedule, reserve concurrency or contact an upstream.
Model selections come from this full registered directory, independently of the
administrator's downstream key restrictions; per-credential model queries are removed.
CPA owns all key lifecycle operations.
API keys are masked to the first six and last four characters (keys of ten or
fewer characters display `***`); original credential names remain readable.
Credential labels expose the host-provided `auth_index`, including group-level
indexes for keyless providers. Policy references hash the scheduler `id`.
CPA v8.0.45's Host model callback lacks source/status, while its credential
inventory omits some configured/disabled runtime entries. The pinned
configuration-to-ID adapter remains a metadata bridge, with every fingerprint,
provider and index checked against the authoritative Host model result. File credentials
use actual host IDs, including relative auth-directory paths. Unmatched metadata,
missing indexes and duplicate runtime IDs fail the whole sync;
shared indexes do not collapse distinct scheduler credentials. Indexes are display
metadata, not a replacement for the policy's credential identity.
One narrow exception is a fully validated Host-only entry with a strictly empty
model array: it cannot own a model and is not included in the persisted catalog.
The UI does not infer its source or enabled status. Known metadata entries with
empty models are retained; unknown nonempty owners, malformed/duplicate entries,
known metadata mismatches and missing Host coverage still reject the full sync.
The success notice and health area show the count of entries not included for
this reason. This browser observation is tied to the confirmed catalog revision,
not persisted as credential status or policy.
Plugin-owned management and resource paths retain CPA's required v0 namespace.
Generated display labels are not written into policy. Credential categories come
from the current CPA inventory. Obsolete policy selections remain visible for
removal, but cannot be newly selected.
The UI submits edited fields only when Save is clicked, without publish or rollback buttons.
Closing an editor discards changes only after confirmation. Groups may include an
optional string `note`, independent of `name`; omitted and empty notes are valid.
Writes still use revision preconditions; failed saves do not display success or
discard the edit. Rule IDs remain internal and are not shown in the rule list.
The rollback route remains available to external administrators for recovery, but is
intentionally absent from the lightweight control-panel workflow.

Synchronization double-reads configuration, normalized credential identity/status,
and registered model sets before publishing. A detected change aborts publication.
The full normalized Host set participates, including every excluded model-free
entry's fingerprint, index, provider and empty model set; identity changes,
additions/removals and transitions to a nonempty model set abort publication too.
CPA has no global registry revision, so this detects observed changes rather than
providing a cross-API atomic snapshot. Opening the panel and ordinary sync never
refresh model sources. The explicit “刷新 CPA 模型并同步” action POSTs
`/v8/management/models/refresh` once, requires HTTP success and `ok: true`, then
synchronizes and confirms catalog health. A successful CPA refresh cannot be rolled
back by a failed plugin sync; the UI reports that partial outcome and retains edits.

## Synchronization

- An administrator sends a complete snapshot, never a patch assembled by the plugin.
- The plugin validates then swaps the snapshot atomically.
- The administrator reads `/health` after every write and marks synchronization failed
  unless the returned revision matches the requested revision.
- The plugin must not call back into an administrator on the request hot path.

## Credential model catalog

`GET /model-catalog` returns `{"snapshot": <document or null>, "health": {...}}`.
It returns 200 even before synchronization so a manager can bootstrap the
directory; `health.status` is `unavailable` and the initial ETag is `"0"`.
`PUT /model-catalog` uses this independent revision as `If-Match`, a unique
`Idempotency-Key`, and the complete next-revision document:

```json
{
  "contract_version": "v1",
  "plugin_version": "0.1.9",
  "cpa_version": "v8.0.45",
  "catalog_revision": 1,
  "generated_at": "2026-10-28T00:00:00Z",
  "credentials": []
}
```

Each credential contains `ref` (the existing fingerprint), `source`, lowercase
`provider`, boolean `enabled`, and a non-null `models` array of registered public
IDs. See `model-catalog.schema.json`. No keys, auth IDs, indexes, labels, or
account data are accepted. Empty complete directories are valid; duplicate refs,
duplicate model IDs (case-insensitive), unknown fields and malformed entries
reject the whole transaction. Success returns `{"catalog_revision": <revision>}`.
Revision conflicts and persistence failures have the same 409/500 semantics as
policy transactions. Unreadable/corrupt catalog state returns 503 on writes and
requires backup recovery while CPA is stopped. Uncertain durability also removes
the active catalog until restart reconciles disk state.

`/health.model_catalog` reports availability, revision, generated/accepted times,
age, credential count, last rejection, snapshot `plugin_version`, snapshot
`cpa_version` and `refresh_required`. Valid format-1 v1 catalogs from producer
0.1.8/CPA v8.0.32 remain readable with their revision, timestamps and receipts
unchanged and `refresh_required: true`. Startup does not rewrite them. New writes
require the current 0.1.9/v8.0.45 envelope and the next revision; exact historical
idempotency retries still return their original receipt. Unknown versions or
corrupt state remain errors. This is independent of policy health:
model-only policies and unrestricted callers do not need credential associations.
There is no automatic expiry; management outages retain the last accepted catalog.
The panel synchronizes on opening and on its sync button. It rechecks CPA config
and credential routing identities/model sets before publishing; observation timestamps and
transient cooldowns are ignored. The host offers no atomic registry revision, so
changes to credentials, registered models, aliases or prefixes require resync.

For a caller with credential restrictions, each model permitted by its model rule
must be present in the saved directory and have an enabled, permitted credential.
Shared models remain visible with any permitted owner. Disabled keys and explicit
model denial remain restrictive even without a catalog. Missing or incomplete
associations return `model_catalog_unavailable` or `model_catalog_out_of_sync` in
the model-list JSON error and `X-CPA-Helper-Error`; HTTP remains 200 under the host
response ABI. Header-identity fallback is unchanged. The actual scheduler remains
authoritative for generation, priorities and transient availability.

## Storage and recovery

`state_dir` defaults to `plugins/cpa-helper-state`; its parent must exist. CPA's
host-managed `store` installation metadata is accepted but ignored by the plugin.
Other unknown plugin configuration fields are rejected before any policy state is
opened. Only a newly created directory is bootstrapped with revision 1 and explicit
unrestricted unknown-key behavior. An existing directory with missing/corrupt state
is not reset.
The `state.json` transaction includes current policy, previous policy and
idempotency receipts. The independent `model-catalog.json` transaction contains
the current catalog and its own durable receipts. Catalog writes do not advance
policy revisions or change policy rollback history. Writes use a synchronized temporary file and replacement in
the same directory. Keep the directory on a local filesystem with atomic rename.
No shared-state/multi-process or multi-node concurrency coordination is supported.
Do not point multiple plugin instances at the same directory.

Back up the entire directory before upgrading. To recover unreadable state, stop
CPA and restore a verified backup; health remains diagnostic while requests fail
closed. Planned policy changes use the authenticated API, not direct file edits.

## External administrator example

Read `/policy` and its ETag, edit the returned document, increment
`policy_revision`, set `generated_at`, then PUT it with the ETag and a unique
idempotency key. After success compare `/health.policy_revision` with the response.
CPA-Helper may implement this workflow later; no billing or usage queue is consumed
by this plugin. Failed synchronization leaves the last accepted policy active.
