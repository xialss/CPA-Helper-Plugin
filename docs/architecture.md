# Architecture

## Boundaries

```text
cmd/cpa-helper-plugin/  C ABI entry point and official host-table bridge
internal/plugin/       lifecycle adapters, management handlers and embedded UI
internal/policy/       key, reusable group and model/credential policy logic
internal/snapshot/     validation, atomic persistence, idempotency and rollback
internal/runtime/      in-flight reservations and authorized-subset selection
```

The exact package names may change, but dependency direction must remain:

```text
ABI -> plugin -> policy/snapshot/runtime
```

Policy code must not know about CPA C function tables or HTTP transport. Adapter code
translates external payloads into domain inputs and translates domain decisions back
into CPA responses.

## Control planes

- CPA official plugin management owns registration, lifecycle, plugin resource routing,
  and host capabilities.
- An external administrator may own users, API-key groups, model rules, synchronization,
  and audit views. It is not part of this repository.
- The plugin owns local policy state and enforcement on every CPA request.

Neither control plane is allowed to become a hidden request proxy. The request path
stays inside CPA so streaming, cancellation, retries, and upstream scheduling retain
CPA semantics.

## Request lifecycle

1. CPA authenticates the downstream API key and supplies caller metadata.
2. The pre-request hook resolves the caller to a key/group policy.
3. The policy engine evaluates the requested model and returns an allow/deny decision.
4. If allowed, the plugin may reserve concurrency and let CPA select an upstream
   credential according to CPA's scheduler contract.
5. The completion hook releases reservations, including cancellation and error paths.
   Reservations are keyed by request ID, survive policy changes, and are acquired
   only once across repeated interception. An in-progress intercept is fenced when
   completion arrives concurrently. Count-token operations do not acquire slots.
6. CPA continues delivering usage to existing observers. This release registers no
   usage hook; CPA-Helper remains the billing owner.

Denials must be deterministic:

- `401`: caller authentication is missing or invalid (CPA-owned when possible)
- `403`: caller is authenticated but the model is forbidden
- `429`: quota or concurrency policy rejects the request
- `503`: no valid policy or compatible upstream can serve the request

The response body should contain a stable machine-readable error code and a safe human
message. Never include the raw API key or upstream credential details.

## Policy model

The first schema should support:

- `policy_revision`: monotonically increasing JSON-safe integer
- `keys`: hashed key identifier, enabled state, group bindings and concurrency limit
- `groups`: group identifier and model rules
- model rules with explicit allowlist and denylist semantics
- `generated_at`, `contract_version`, and `plugin_version`; v1 has no expiry

Deny rules take precedence over allow rules. An absent allowlist means all models are
eligible unless denied. An empty allowlist is restrictive only when the schema marks it
as restrictive; do not infer semantics from an empty JSON array alone.

## Failure and recovery

- Validate the whole snapshot before replacing the active snapshot.
- Write the accepted snapshot atomically and keep the previous revision for rollback.
- Reject stale revisions unless an explicit administrative rollback is requested.
- Expose active revision, last synchronization time, policy age, and last error.
- Do not fail open because an external administrator cannot be reached.

An accepted policy applies to subsequent hook evaluations. Already running upstream
calls are not canceled on policy change. Lowering a concurrency limit prevents new
admissions until existing calls finish. Plugin quiesce stops admission and leaves
completion cleanup active; lifecycle disable/unload is controlled by CPA.
