# DSH credential catalog cross-review

Reviewed on 2026-10-18 against CPA v8.0.32
(`911f49cbd49f4fe2516979754a7488a3fd14fe64`), plugin 0.1.8,
C ABI 1 / RPC schema 6. This completes the review interrupted in the
"CPA Plugin DSH 兼容" session.

## Resolved finding: P2 catalog JSON field ambiguity

The catalog management boundary accepted case-insensitive field aliases and
duplicate fields. For example, `"enabled":false,"enabled":true` could silently
select the latter value. This contradicted the catalog schema and the existing
strict policy decoder. The management API remains authenticated; this defect
affected catalog acceptance and visibility, while generation admission remained
authoritative inside CPA.

`internal/catalog/json.go` now validates exact field names, uniqueness, required
fields, non-null values and types for both the snapshot and credential objects.
Decoding uses temporary values, so rejection cannot mutate the caller's snapshot.
Invalid management documents return HTTP 400 without advancing the catalog
revision or replacing its active directory. Ambiguous stored snapshots remain
unavailable and require recovery while CPA is stopped.

Regression coverage includes direct decoding, management rejection without
changing a disabled credential's visibility, persisted-state recovery, and real
CPA management forwarding of alias and duplicate-field rejection.

## Independent review

The independent reviewer confirmed the decoder and its regression coverage,
checked the directory synchronization transaction and effective credential
filtering, and found no additional confirmed defect. The reviewer independently
passed seven related Go tests, nine JavaScript directory tests and
`git diff --check`, without editing files.

## Verification

On Windows amd64 with Go 1.26.4 and UCRT GCC 16.2.0:

- Changed Go sources passed `gofmt`; `go vet ./...`, `go test ./...` and
  `go test -race ./...` passed.
- The real CPA v8.0.32 compatibility fixture passed with the newly built DLL,
  including strict catalog HTTP rejection, list/detail visibility, alias and
  credential routing, weights, concurrency, streaming and lifecycle recovery.
- The nine directory tests and Edge browser smoke passed, including complete
  synchronization, failed-query handling, public prefixes and credential rules.
- The official CPA archive checksum and extracted executable were verified.

Artifact: `dist/repair-v0.1.8/cpa-helper-plugin.dll`.
SHA-256: `3f9091ae4bd26a89c2a5bf320404d6d7aa7b52f5ce39ae8961c7f178802b09d9`.

## Compatibility, rollback and remaining limits

The ABI, policy contract, catalog contract and storage format remain unchanged.
Existing canonical snapshots require no migration. Restoring a pre-review 0.1.8
DLL removes the stricter decoding; corrupt catalog state must be restored from a
valid backup while CPA is stopped. See `compatibility.md` for version rollback.

CPA provides no atomic registry revision. Credential/model changes still require
explicit resynchronization, and synchronization cannot certify a completely
stationary host registry. The existing header-identity relaxation and HTTP 200
model-list error-body limitation remain. Hidden model details use CPA's 404 path.
Linux/Docker, a live DSH profile and external provider calls were not verified.
All runtime tests used isolated CPA instances and synthetic credentials.
