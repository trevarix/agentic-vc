# AVC Implementation Plans — Overview

**Date:** 2026-07-09
**Source:** [code-review-2026-07.md](../code-review-2026-07.md)
**Status:** Proposed

The July 2026 code review is broken into six implementation plans, in dependency order. Each plan is a standalone document with per-item detail and exit criteria. **Do not start a plan until the previous plan's exit criteria are all green** — later plans multiply the load on the exact code paths earlier plans fix (continuous snapshotting multiplies snapshot volume; the merge train multiplies merges).

| # | Plan | Theme | Review items | Est. duration |
|---|------|-------|--------------|---------------|
| 01 | [Data Safety](01-data-safety.md) | Never lose user data | 1.1, 1.4, 2.1, 2.2, 2.6 | ~1 week |
| 02 | [Merge Integrity](02-merge-integrity.md) | Merge never crashes, always recoverable | 1.2, 1.3, 2.7, 2.8 | ~1 week |
| 03 | [Lifecycle Hardening](03-lifecycle-hardening.md) | Retention/GC/diff/web correctness | 2.3, 2.4, 2.5, 2.9, 2.10 a–i | ~1–2 weeks |
| 04 | [Trust Primitives](04-trust-primitives.md) | Every operation reversible, every agent bounded | A1–A4 | ~3 weeks |
| 05 | [Agent-Era Features](05-agent-features.md) | The features no git wrapper can match | B1–B4 | ~4 weeks |
| 06 | [Ecosystem & Adoption](06-ecosystem.md) | Meet teams where they are | C1–C4 | ~4 weeks |
| 07 | [Sandbox Containment](07-sandbox-containment.md) *(conditional)* | Workspace runner becomes a real OS-enforced boundary | Runner assessment follow-up | ~3–4 weeks |

Effort key (per item): **S** < 4 h · **M** 1–3 days · **L** 3–7 days · **XL** 1–2 weeks

## Dependency notes

- **01 before everything.** Torn object writes (2.1) and the restore deletion sweep (1.1) are data-loss bugs; nothing should ship on top of them.
- **02 before 05.** The merge train (B4) is pointless while deletion merges panic (1.2) and abort is broken (1.3).
- **03 before 05.** `avc watch` (B1) multiplies snapshot volume — retention must stop deleting merge bases (2.3) and GC must stop racing snapshots (2.4) first.
- **04 A4 (fsck + compression) before 05 B1.** Continuous snapshotting on an unverified, uncompressed object store compounds both risks.
- **03 item 2.10f (web hardening) before 06 C4.** The web cockpit must not be built on CSRF-able endpoints.
- **07 is conditional, not sequential.** The workspace runner today is a hygiene layer, not a security boundary (03·5k documents this honestly). Plan 07 upgrades it to OS-enforced containment and is triggered by usage, not order: start it when users run untrusted third-party test suites through the runner, or when Plan 05's fleet automation (B2 scheduled bisects, B4 `--train --validate`) executes commands against unreviewed branches. Its Phase 1 (launcher interface) and the Windows job-object sub-phase are cheap and worth pulling forward regardless.

## Global acceptance criteria (every item in every plan)

1. `go test ./...` passes (`-race` where CGO is available; omit on Windows without CGO).
2. New tests cover the happy path and at least one failure path. The three reproduced bugs (1.1, 1.2, 1.4) each get a regression test that mirrors the review's repro script.
3. Every new CLI command/flag supports `--json`, and errors go to stderr with exit code 1.
4. Every new or changed MCP tool has an accurate, agent-readable description.
5. Architecture rules in [CLAUDE.md](../../CLAUDE.md) hold: logic in `internal/`, thin `cmd/` files, one DB connection per operation, write-once objects.
6. `docs/cli-reference.md` and `docs/architecture.md` updated for any user-visible or structural change.
