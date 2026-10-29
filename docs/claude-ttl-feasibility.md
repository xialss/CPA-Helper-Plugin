# Claude TTL feasibility verification

2026-10-29: the user requested removal of TTL. Its implementation, diagnostic
probe and dedicated tests have been deleted from the working tree. This document
is historical evidence; use [the removal record](claude-ttl-removal.md) for the
current behavior and external configuration cleanup.

This document preserves the feasibility-stage evidence. Subsequent implementation
is tracked separately in [the uniform TTL plan](claude-uniform-ttl-plan.md) and
[the deployment/metadata contract](claude-uniform-ttl.md); statements below about
"not implemented" describe the experiment's baseline, not the later working tree.

Status: verified against the official Windows amd64 CPA v8.0.45 binary on
2026-10-28. The user subsequently excluded mixed TTLs within one request. Under
the revised scope, **uniform request TTL enforcement passes the tested paths**.
The original mixed-preserving counterexamples remain below as historical
evidence, not blockers for uniform normalization. The stock usage queue still
does not expose TTL; a metadata integration has not yet been implemented or
verified. This is a feasibility result, not a production implementation.

## Revised scope: one TTL per request

User clarification, 2026-10-28: each request is entirely 5m or entirely 1h;
support for mixed 5m/1h requests is not required.

The tested candidate lets the authenticated Key policy select one TTL for the
whole request. Late `override` sets every existing ephemeral breakpoint to 1h,
or late `filter` removes TTL from every such breakpoint for bare 5m. It changes
TTL values, not breakpoint placement; `prompt_cache_options.mode=explicit`
with no breakpoint remains empty. This explicitly makes Key policy win over
conflicting incoming TTL values in this candidate. The old requirement to
preserve different per-block TTL values does not apply to this candidate;
production PRD wording must state the precedence before implementation.

`TestClaudeUniformTTLFeasibility` adds 64 requests across the default and
Claude Code CLI fingerprint profiles:

- Native Claude, ordinary and streaming: no controls, three bare controls,
  three explicit 5m controls, three explicit 1h controls, explicit mode without
  controls, and explicit mode with three 5m or three 1h controls.
- OpenAI Chat forwarded to Claude, ordinary and streaming.
- Both authenticated fixture keys, each applied to every input case.
- Every final control is asserted as bare 5m or explicit 1h according to the
  authenticated Key. No-breakpoint explicit mode is asserted to stay empty.
- A cooperative synthetic upstream reports 120 cache-creation tokens entirely
  in the selected bucket, or zero for no controls. The fixture checks that CPA
  preserves this aggregate, while its official usage hook and queue still omit
  TTL. This mock is not evidence of a real upstream honoring the requested TTL.

Both configurations passed. No early body rewrite, per-request JSON parsing in
the plugin, or `prompt_cache_options.mode` workaround is needed for this
candidate. The existing after-auth body-omission optimization can stay enabled
in a future marker-only implementation. The old late-`default` candidate is
not the proposed uniform implementation.

### Revised decision

| Requirement | Revised assessment |
| --- | --- |
| Mixed bucket quantities and mixed TTL ordering | Out of scope; no longer a feature blocker |
| Uniform per-Key request TTL | Mechanism verified for the two profiles, native Claude and Chat, ordinary and streaming |
| Cache-creation quantity | Continue using CPA's authoritative aggregate; no retokenization or second counter |
| Price all created tokens at the single request TTL | Possible as request-policy attribution once the frozen execution TTL is reliably associated with the usage record; not verified end to end |
| Add TTL directly to the unchanged stock queue | Still unsupported by the public observation callback |
| Claim upstream-reported actual TTL without receiving it | Still invalid; the revised scope does not create upstream evidence |

### Proposed integration to verify next

Keep `usage.handle` as the sole usage observation point and keep all token
quantities in the original CPA queue. A future plugin metadata interface could
associate the hook's usage RequestID with the execution's frozen TTL and policy
revision. Helper would join that metadata to the stock queue's `execution_id`.
The supplemental metadata would contain no second token count or billing
ledger. This avoids relying on translated response buckets and does not
require modifying CPA.

This is a candidate design, not a completed proof. Required follow-up gates:

1. Freeze TTL and policy revision for the request; do not look up the Key's
   current preference when delayed usage arrives. All retries must use the
   same frozen request TTL.
2. Verify the association between request interception and `usage.handle`
   under concurrency, policy updates, retries, fallback, cancellation and
   delayed callbacks. The earlier RequestID mismatch still exists. Parent
   TraceID alone is not a proven unique per-attempt join; ambiguous or missing
   associations must remain observable as unknown.
3. Define authenticated metadata lookup, delivery/recovery and missing-data
   semantics without modifying the stock queue or duplicating its quantities.
4. Mark the provenance explicitly, for example `ttl_source=enforced_request`,
   rather than pretending request policy is upstream-reported TTL. If the
   original Helper field remains defined as actual upstream TTL, it cannot
   silently acquire this new meaning; adapt the contract before pricing by it.
5. Verify required payload rules, rule precedence and configuration drift before
   reporting a preference as effective. A cooperative mock does not certify
   real Anthropic acceptance, provider behavior, headers or cache hits.

Thus the current decision is to proceed with a uniform-TTL design, with the
metadata/contract integration as a separate unverified gate. It is no longer
accurate to block the request-side feature on mixed buckets or preservation of
different per-block TTLs. No production code or Helper task state is changed
by this scope adjustment.

## Scope

Verify the unchanged official CPA v8.0.45 binary against a diagnostic plugin
built with its public SDK (C ABI 1 / RPC schema 7). Production Plugin v0.1.9,
CPA source, CPA-Helper source, deployment configuration and real credentials
remain unchanged. Synthetic upstream replies prove transport and hook behavior,
not Anthropic acceptance, actual cache hits or monetary billing.

The historical experiment below used the original per-caller `5m` / `1h`,
explicit TTL preservation and actual upstream bucket requirements. The revised
single-TTL scope above supersedes the mixed-preserving acceptance items.
Request-policy attribution must remain distinct from upstream-reported evidence.

## Verification questions

1. Does an after-auth request interceptor receive the authenticated caller scope,
   and do its returned headers reach final header-scoped payload rules?
2. Do request-body rewriting and final payload rules survive CPA's automatic
   cache-control placement, TTL upgrading, stripping and normalization?
3. Which response hooks retain actual 5m / 1h buckets and RequestID, for native
   Claude and Claude-to-OpenAI, ordinary and streaming requests?
4. Does `usage.handle` retain those buckets, and can its return value augment
   the existing CPA queue record?
5. Can identical concurrent requests from different callers be correlated
   without hashing request bodies or inferring usage from policy?

## Isolation

The opt-in probe only collects in-memory sanitized counters, opaque caller
scopes, execution IDs and hook names. It does not persist request bodies or API
keys, provide billing, publish supplementary usage, or modify the shipped plugin.
Its management endpoint is protected by CPA management authentication. The test
creates temporary loopback CPA configuration and synthetic credentials, and
terminates its CPA process on completion. It never loads deployment state.

## Environment and reproducible experiment

- Plugin baseline: v0.1.9; production implementation is unchanged.
- CPA: v8.0.45, commit `ad8bdd3f20bef44f0684b8a6096a216b2c76040d`;
  official executable reports build time `2026-10-27T15:27:53Z`.
- Official Windows archive SHA-256, verified when installing the fixture:
  `b049739ca8cd1885c59bdf57de6adf12452e9ab9907f50117906c11fadffff1b`.
- Probe: public SDK only, C ABI 1 / RPC schema 7; Go 1.26.10 and UCRT GCC 16.2.0.
- Six isolated configurations: default API-key wire profile and opt-in
  `claude-code-cli` fingerprint with cloaking, each with no payload rule,
  late `default`, or late `override`. Neither profile uses a real OAuth token.
- Each configuration tests seven request bodies, both caller policies, marker
  only versus body rewriting, a spoofed marker, four non-Claude requests, and
  eight identical-body concurrent requests. Total: 246 synthetic requests,
  including 48 concurrent native/translated ordinary/streaming requests.

The synthetic upstream emits aggregate cache creation `120`, with actual test
evidence `ephemeral_5m_input_tokens=80` and `ephemeral_1h_input_tokens=40`.
Streaming emits these counters in both `message_start` and `message_delta`;
the stock aggregate remains 120 rather than summing cumulative observations.
These fixed values are fixture data, not measurements of a real Claude call.

The published production plugin 0.1.9 does not register `usage_plugin`; the probe deliberately
does so to inspect the available observation contract. It does not add a queue,
modify accounting, or return an enriched billing event.

## Request-side results

| Check | Observed final upstream behavior | Decision |
| --- | --- | --- |
| Authenticated caller | After-auth hook receives distinct opaque caller scopes for the two fixture keys | Supported |
| Existing bare breakpoint, default profile | Body rewriting changes bare ephemeral to explicit 1h | Supported for this path |
| No client breakpoint | Body rewriting alone leaves CPA-generated controls at bare 5m in the default profile | Insufficient |
| CLI fingerprint, no late rules | Caller configured as 5m still gets two 1h controls, even with body rewriting enabled | Insufficient |
| Header-scoped late rules | Plugin marker reaches payload matching; default-profile auto controls become 1h for the 1h caller; CLI auto controls become bare 5m for the 5m caller | Mechanism works |
| Client spoofs 1h marker using 5m key | Authenticated plugin marker wins; late rules yield 5m | Supported in fixture |
| Explicit cache mode with no breakpoint | All tested configurations keep zero controls | No implicit breakpoint added |
| Explicit client 1h, caller 5m | The naive late filter removes the client's explicit 1h | Violates preservation requirement |
| Explicit client 5m, caller 1h | Late override replaces explicit 5m with 1h | Violates preservation requirement |
| Mixed ordering | Late default can produce `5m,1h,bare-5m`, after CPA has already normalized TTL ordering | Violates required TTL order |
| Non-Claude | Upstream OpenAI bodies are byte-identical between marker-only and rewrite modes for both keys | TTL branch leaves this tested path unchanged |

The naive late-rule candidates intentionally remove TTL from every matching
ephemeral block for 5m, and default or override TTL to 1h for 1h. They prove the
late barrier exists; they are **not deployment recipes**. The marker is not
forwarded to the synthetic Claude upstream in the tested configuration.

For the mixed-order counterexample, the client supplies a 5m tool breakpoint,
a bare system breakpoint and a 1h message breakpoint. With no late rules CPA
normalizes the wire to `5m,bare-5m,bare-5m`. With marker-only interception, the
late default then adds 1h to
the system block while preserving the first explicit 5m, producing the invalid
ordering above. A successful HTTP response from the permissive mock does not
make that payload acceptable to Anthropic.

Enabling the early body rewrite changes this specific late-default result back
to `5m,bare-5m,bare-5m`: the rewritten original now contains an explicit 1h
that CPA later strips, and the default rule skips that source path. Thus the
early-rewrite/late-default combination also cannot be assumed to guarantee 1h.

The CLI profile also changes client system content and placement as part of
CPA's existing cloaking behavior. Its rows are observations of the whole CPA
pipeline, not an assertion that the probe alone removed the explicit control.

Consequences for a future request-only design:

- Keep selection in the authenticated plugin policy; never trust the client's
  marker or put raw caller API keys in static payload rules.
- Prefer a small fixed set of official rules over one rule per raw API key, but
  do not ship these naive rules. Preservation, breakpoint provenance, mixed TTL
  ordering, subagents/helpers and configuration drift need a separate design.
- Changing the `prompt_cache_options.mode` to `explicit` merely to bypass CPA
  would change placement semantics; this is not an accepted workaround.
- A body-based after-auth implementation would need to disable the shipped
  `request_interceptor_omit_body_after` optimization. A marker-only hook can
  keep it, but cannot by itself promise the final TTL when rules are absent.
- Do not report a saved preference as effective until the required mechanism
  and its final-payload guarantees have passed their own acceptance tests.

## Usage observation and identity results

| Path / hook | Actual 80/40 buckets | Execution identity | Meaning |
| --- | --- | --- | --- |
| Native Claude ordinary: `response.intercept_after` | Present | RequestID and caller scope present | Limited raw observation possible |
| Native Claude stream: `response.intercept_stream_chunk` | Present in start/delta | RequestID and caller scope present | Limited raw observation possible; counters are cumulative |
| Claude upstream to OpenAI Chat ordinary: `response.normalize_before` | Present | DTO has no RequestID or caller scope | Cannot directly associate evidence with an execution |
| Claude upstream to OpenAI Chat ordinary: after interceptor | Absent | RequestID and caller scope present | Translation has discarded buckets |
| Claude upstream to OpenAI Chat stream: before normalizer | Present | DTO has no RequestID or caller scope | Same association gap |
| Claude upstream to OpenAI Chat stream: chunk interceptor | Absent | RequestID and caller scope present | Same loss after translation |
| `usage.handle`, all tested paths | Absent; aggregate=120 | Usage RequestID and TraceID present | Single official usage observation point cannot supply TTL buckets |
| Official `/v0/management/usage-queue` | Absent; `tokens.cache_creation_tokens=120` | Queue execution_id matches usage RequestID | Canonical TTL fields cannot be consumed because they are not emitted |

All 48 concurrent responses retain the matching caller scope and request
interceptor execution ID. Their bodies are identical across the two callers;
there is deliberately no body-digest correlation. This verifies the tested
identity-bearing hooks, not a universal raw-before-translator join.

There are three ID meanings that must not be conflated:

1. Request/response interception uses its own execution RequestID.
2. `usage.handle.RequestID` is a different usage execution UUID in these runs.
3. The stock queue stores that usage UUID as `execution_id`; its `request_id`
   comes from the inbound logging context, and `trace_id` is the parent trace.

The interceptor and usage TraceIDs match in the simple one-attempt cases, but
this is only a parent correlation. It does not prove a unique per-attempt join
under retries, fallback or multiple executions. Timing/order, prompt hashes and
the requested TTL are not valid substitutes for missing usage evidence.

The SDK `UsageDetail` has only aggregate token counters, and `HandleUsage`
returns no enriched record. The core parser ignores the nested TTL buckets and
the queue serializes a fixed `tokenStats` structure. Returning extra JSON from
an observer would not add fields to the stock queue. The probe therefore does
not fabricate `cache_creation_ttl`, `cache_creation_5m_tokens`, or
`cache_creation_1h_tokens`.

## Decision for the cross-workspace requirement

| Original acceptance item | Feasibility under current constraints |
| --- | --- |
| Store and authorize per-owner TTL preference | Ordinary Plugin/Helper work; not implemented or owner-tested by this experiment |
| Guarantee final per-Key TTL on all supported Claude paths while preserving explicit semantics | Partial mechanism proven; full guarantee not proven and naive candidates have concrete failures |
| Add actual mixed TTL buckets to unchanged CPA usage detail / queue through a plugin observer | Not supported by current public contract |
| Supplement all queue records using a separate raw-response observer | Not accepted: violates current single-usage-point architecture and lacks a proven general translated-response/attempt association |
| Claim exact TTL billing from the requested setting | Rejected: request intent is not actual usage evidence |

Under that original scope, these acceptance items were blocked. The explicit
user clarification now removes mixed support as described in the revised
section. The overall feature is still not implemented, and this verification
does not change the Helper task's planning status or either production application.

Native-only diagnostic observation could be considered separately, but is not
the requested cross-protocol billing feature and is not implemented here.
Replacing CPA's executor/translator or introducing a proxy would materially
expand the architecture; neither is proposed as an implicit compatibility fix.

## Reproduce and validate

Run in this repository using the Go version from `go.mod` and an appropriate
C compiler. The official CPA binary must already be available locally:

```powershell
$env:CGO_ENABLED = '1'
# Set CC/PATH to the locally installed, matching-architecture C compiler.
go build -buildmode=c-shared -o dist/ttl-probe.dll ./integration/testdata/ttlprobe
$env:CPA_BINARY = 'C:\path\to\v8.0.45\cli-proxy-api.exe'
$env:CPA_TTL_PROBE_BINARY = (Resolve-Path dist/ttl-probe.dll).Path
go test -v ./integration -run '^TestClaudeTTLFeasibility$' -count=1 -timeout 120s
# Revised one-TTL-per-request proof:
go test -v ./integration -run '^TestClaudeUniformTTLFeasibility$' -count=1 -timeout 120s
```

Linux uses a matching official binary and `.so` probe. Linux and real OAuth
credentials were not tested in this verification. Normal `go test ./...` skips
the opt-in proof unless both environment variables are set; a skipped proof
must never be described as a CPA feasibility pass.

The fixture asserts the decisive default-profile wire outcomes, CLI automatic
TTL outcomes, marker spoof protection, caller identities, non-Claude body
preservation, and actual official queue aggregates/field absence. Hook matrices
are printed as sanitized summaries. A passing proof means the recorded
positive and negative observations are reproducible, not that the original
feature meets its acceptance criteria.

### Completed checks

| Check | Result |
| --- | --- |
| Build diagnostic DLL using the public SDK | Passed |
| Opt-in `TestClaudeTTLFeasibility` against official CPA | Passed, all six configurations / 246 synthetic requests |
| Revised `TestClaudeUniformTTLFeasibility` against official CPA | Passed, both configurations / 64 synthetic requests; uniform request TTL, authoritative totals, no TTL in stock queue |
| `TestCPACompatibility` using the original released v0.1.9 DLL | Passed; real dynamic loading and management contract smoke |
| `go vet ./... ./integration/testdata/ttlprobe` | Passed |
| `go test ./...` | Passed; environment-gated real-binary tests skipped in this separate default run |
| `go test -race ./...` | Passed; same opt-in skips, diagnostic DLL is not race-instrumented |
| `gofmt -l` on all three new Go fixture/probe files | Passed, no output |
| `git diff --check` | Passed |

All isolated CPA processes have exited. Only the proof sources and related
documentation changed; no production source, Helper/Trellis state, deployment,
commit, PR or release was changed by this feasibility verification.

## Boundaries and remaining unverified work

- No real Anthropic endpoint, cache hit, bill, minimum-cache-length acceptance,
  account entitlement or live pricing was tested; the upstream is synthetic.
- No new retry/fallback/cancellation or subagent/helper production guarantee is
  claimed. The general translated-response evidence gap and stock queue schema
  already block the full requirement before such acceptance work.
- Responses/Gemini client translations and actual OAuth credentials have not
  been certified by this proof; the tested translated client is OpenAI Chat.
- Owner UI/API, transactional TTL policy updates, drift detection, Helper
  ingestion changes and migration/rollback are not implemented here.
- Helper's management-version whitelist still needs separate reconciliation
  with Plugin v0.1.9 if future integration proceeds; this proof does not alter it.

## Source anchors

The Context7 lookup failed with `fetch failed`; conclusions use the pinned
primary source and observed official-binary behavior, not remembered API shapes.

- `sdk/pluginapi/types.go`: request interception, response transforms,
  response/stream interception, `UsageRecord` and aggregate-only `UsageDetail`.
- `internal/pluginhost/rpc_client.go`: response normalization does not receive
  the identity-bearing request-interceptor DTO.
- `internal/runtime/executor/claude_executor_execute.go` and
  `claude_executor_stream.go`: placement/upgrade/normalization occurs
  before the final header-scoped payload barrier.
- `internal/runtime/executor/claude_executor_cloaking.go`: explicit mode,
  TTL upgrade/stripping and tools-system-messages ordering normalization.
- `internal/runtime/executor/helps/payload_helpers.go`: header matching,
  original-payload-aware defaults, JSON query paths and overrides/filters.
- `internal/runtime/executor/helps/usage_helpers.go`: aggregate Claude parser.
- `internal/pluginhost/adapters_usage_translation.go`: aggregate-only mapping.
- `internal/redisqueue/plugin.go`: fixed tokenStats and queue ID semantics.
- Pinned source: <https://github.com/router-for-me/CLIProxyAPI/tree/v8.0.45>.
- Upstream TTL contract:
  <https://platform.claude.com/docs/en/build-with-claude/prompt-caching>.
- Cross-workspace requirements:
  `D:/Project/CPA-Helper/.trellis/tasks/10-27-claude-ttl-request-rewrite/prd.md`;
  billing consumer contract:
  `D:/Project/CPA-Helper/.trellis/spec/backend/usage-pricing.md`, section 28.
