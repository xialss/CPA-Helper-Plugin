# Management Contract

This repository implements only the CPA plugin. An external administrator may use
these endpoints to configure it, but no administrator-specific code, database, UI, or
runtime dependency belongs here.

CPA's official plugin management surface remains responsible for plugin registration,
lifecycle, and resource routing. The endpoints below are the plugin's generic policy
management surface.

## Endpoints

Model-list filtering is controlled through CPA's official plugin configuration:
`PATCH /v0/management/plugins/cpa-helper-plugin/config` with
`{"model_list_filter_enabled":false}` disables it; true enables it (the default
when omitted). This host-managed setting persists across restarts and does not
modify the policy snapshot or disable generation enforcement. `/capabilities`
reports its current value, target CPA v7.3.8 and plugin version 0.1.2.

The v1 prefix is `/v0/management/plugins/cpa-helper-plugin/v1`. CPA's management
middleware authenticates every request using its management key. The plugin does
not open an additional listener or accept downstream API keys for administration.

| Method | Resource | Purpose |
| --- | --- | --- |
| `GET` | `/health` | plugin status, active revision, policy age, contract version |
| `GET` | `/policy` | complete policy with hashed identifiers; ETag is the revision |
| `PUT` | `/policy` | validate and atomically activate a complete snapshot |
| `POST` | `/policy/rollback` | activate a retained revision |
| `GET` | `/capabilities` | supported modules, contract and ABI versions |
| `GET` | `/directory` | host credential inventory plus observed configured credentials, with fingerprints only |

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
  "plugin_version": "0.1.2",
  "policy_revision": 2,
  "generated_at": "2026-09-18T00:00:00Z",
  "groups": [],
  "keys": []
}
```

The authoritative shape is `policy.schema.json`; unknown JSON fields are rejected.
Plugin `0.1.2` accepts snapshots produced by `0.1.0` and `0.1.1`; all three
producer versions are enumerated in the schema. The policy contract remains `v1`,
and existing state does not require migration. A 0.1.2 snapshot can be rewritten
with an older producer version only when rolling the binary back to that version.
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
The UI reads CPA `/v0/management/api-keys`, `/v0/management/config`,
`/v0/management/auth-files`, and `/v1/models`
(the latter with a downstream key). CPA owns all key lifecycle operations.
API keys are masked to the first six and last four characters (keys of ten or
fewer characters display `***`); original credential names remain readable.
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

## Synchronization

- An administrator sends a complete snapshot, never a patch assembled by the plugin.
- The plugin validates then swaps the snapshot atomically.
- The administrator reads `/health` after every write and marks synchronization failed
  unless the returned revision matches the requested revision.
- The plugin must not call back into an administrator on the request hot path.

## Storage and recovery

`state_dir` defaults to `plugins/cpa-helper-state`; its parent must exist. CPA's
host-managed `store` installation metadata is accepted but ignored by the plugin.
Other unknown plugin configuration fields are rejected before any policy state is
opened. Only a newly created directory is bootstrapped with revision 1 and explicit
unrestricted unknown-key behavior. An existing directory with missing/corrupt state
is not reset.
The single `state.json` transaction includes current policy, previous policy and
idempotency receipts. Writes use a synchronized temporary file and replacement in
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
