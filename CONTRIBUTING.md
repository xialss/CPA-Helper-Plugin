# Contributing

Work on a short-lived branch and keep `main` releasable:

```text
feat/<scope>   new behavior
fix/<scope>    defect correction
docs/<scope>   documentation and contracts
chore/<scope>  tooling and maintenance
```

Before a pull request, run the checks in `AGENTS.md`, describe any CPA version or
policy-contract change, and include migration/rollback notes when a persisted snapshot
or management response changes.

Do not commit generated plugin binaries, raw API keys, local policy files, or machine-
specific configuration. A PR that changes ABI-facing code must update
`docs/compatibility.md` and include a compatibility fixture result.
