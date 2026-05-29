# 022 — Divergence from `origin/feat/delta-sync`

**Date:** 2026-05-25
**Status:** Decided (no implementation — meta-log of a branching decision)
**Related:** [[001-progress-projection]], [[013-dialed-gui-cutover]], [[015-design-system]], [[017-stage-bucket-honesty]], [[018-logical-rate-in-ui]], [[019-plan-info-delta]]

## Background

Local `feat/delta-sync` and `origin/feat/delta-sync` forked at `bc1c547` and ran parallel timelines:

- **Local:** 35 commits — dial cutover (#013), rune-* primitives (#015), Advanced disclosure (#014), telemetry hierarchy (#009), run-addresses (#010), Phase taxonomy + ServerReady gate (#017), logical-rate ETA (#018), PlanInfo-delta (#019), Lit purity sweep (#020), livesync (#016), sync-upstream draft (#021).
- **Origin:** 5 commits, all in the transfer-progress / perf space.

Attempted to reconcile (`git merge origin/feat/delta-sync`) — 9 content conflicts + 3 modify/delete in `frontend/src/stages/*` (deleted locally per #013). Took "ours" everywhere; auto-merge then picked up origin's `cmd/gui/main.go` hunks calling `pusher.OnItemDone` and an expanded `projection.New(...)` signature that local types do not expose. Build broke. Aborted.

## Problem

Two branches solve overlapping problems with incompatible API shapes. A clean merge is not achievable by conflict resolution alone — origin's progress-event surface (`ItemDoneInfo`, `applier.OnItemDone`, `pusher.OnItemDone`, `Ticker.SetTransferActive`) requires production-side wiring that does not exist on local. A working merge would mean: keep local for everything except specific origin features cherry-picked surgically.

Question: cherry-pick now and integrate, or stay on local timeline?

## Origin's 5 commits — what each achieves

| SHA       | Subject                                           | One-line summary |
|-----------|---------------------------------------------------|------------------|
| `4440fe8` | `feat(gui): ETA + Applying caption`               | Go-side `ETASeconds` from cumulative AvgMbps; `StartInfo{apply}`/`FinishInfo{apply}` brackets so caption flips to "Applying…" during Pull's disk-bound phase. |
| `4e010eb` | `perf(refs): replace per-blob Exists pre-pass with ref-attested live set` | Replace serial-HEAD pre-pass (~70s on R2 for 700 blobs) with one destination ref-set scan; `transferBlob`'s per-blob `Exists` gate is the runtime safety net. ~70× planning speedup. |
| `9fdf1f6` | `feat(gui): real progress for Apply + Retaining`  | New `ItemDoneInfo{Operation, Weight}` event; Apply emits per blob (`Weight = Object.Size`), Retaining emits per Job (`Weight = 0`). Projection: PlanInfo arrival resets `Done`/`Progress`/`ETA` to stop backward snaps. Composition-root wiring left dormant by design (caller-applied). |
| `76c0682` | `feat(delta-sync): bundle WIP`                    | Eventbus blocking subs, R2 dotenv workflow, apply-progress wiring. No body. |
| `a7949ae` | `fix(gui): honest push progress + heartbeat ticks`| Push bar: drive `BytesDone` from per-blob `ItemDoneInfo` acked after `PutStream` returns, not from compressor read-rate (was racing to 100% then freezing up to 18s during R2 upload). Add `Ticker.SetTransferActive(bool)` so ticker emits heartbeat ticks during R2 stalls (31s TCP-retransmit silences observed). Projection renders "Stalled — waiting on R2…" when `NowMbps==0` mid-transfer. |

## Local coverage — what we already have in the same space

| Concern | Local design log | Origin commit | Comparison |
|---------|------------------|---------------|------------|
| ETA in dial sub | #009 (telemetry hierarchy), #018 (logical-rate ETA), #017 (ETA only during bytes-flowing beats) | `4440fe8` | Local computes from `logicalMbps`; origin uses `AvgMbpsIn`/`AvgMbpsOut`. Local is wired through the unified dial; origin's caption is per-stage-component which is dead code post-#013. **Local supersedes.** |
| Progress-bar denominator honesty | #019 (PlanInfo-delta filters items so bar fills to 100%) | `4e010eb` (eliminates the slow HEAD pre-pass that #019 inherited) | Different problems with the same root: pre-flight `dst.List("objects/")` cost vs. accuracy. #019 made the bar honest; `4e010eb` makes the pre-flight fast. **Complementary — not in conflict.** Worth back-porting. |
| Per-item progress for Apply / Retaining | None — local has no design log for this | `9fdf1f6` | **Genuine gap on local.** Local Apply + Retaining bars still freeze. |
| Honest push (no race-to-100-then-freeze) | Partial via #001 (two counter layers) | `a7949ae` (drive from acked `PutStream` returns) | **Origin is the real fix.** #001's wire layer alone does not solve this — Counter wraps the Compressor, so wire bytes hit total before R2 acks. Worth back-porting. |
| Transfer-stall heartbeat | None — silence guard in `progress.Ticker` is current behaviour | `a7949ae` (`SetTransferActive`) | **Genuine gap on local.** Worth back-porting. |
| "Applying…" inner-phase caption | None | `4440fe8` (apply bracket events) | The Phase taxonomy in #017 collapses Pull+Apply into one `downloading` bucket; whether to surface the inner "Applying…" hint at all is an open #017 follow-up. Defer. |
| Eventbus blocking subs | None | `76c0682` (bundle WIP) | Local livesync (#016) relies on the current async-sub semantics. Origin's blocking-subs change would need #016 re-validation. **Defer.** |
| R2 dotenv workflow | None | `76c0682` | Tooling-only. Trivial back-port. |

## Decision

**Stay on local timeline. Do not merge `origin/feat/delta-sync` into `feat/delta-sync`.**

Reasoning:
1. Local's 35-commit work — dial UI (#007/#013), design system (#015), Phase taxonomy (#017), livesync (#016), sync-upstream (#021) — is structurally further along and not present on origin in any form. A merge in the "theirs" direction would lose those.
2. Local's transfer-progress work (#001/#018/#019) already lands the user-facing wins origin was reaching for (honest ETA, honest bar fill), via different and Phase-aware mechanisms that match the dial UI. A merge in the "ours" direction (already attempted) breaks the build because origin's `cmd/gui/main.go` calls `pusher.OnItemDone` / expanded `projection.New` that local does not expose.
3. Per-file cherry-pick is the right granularity for the four genuine wins origin has (next section), not a branch-wide merge.

## Back-port candidates (deferred to follow-up commits)

Order by value / risk:

1. **`4e010eb` — ref-attested live set perf.** Highest value (≈70× planning speedup), lowest risk (pure-function helper + signature change on `filterMissing`). Conflicts with #019 are syntactic, not semantic; both fixes belong in the same code path. Cherry-pick + reconcile against #019's `dst.List` call.
2. **`a7949ae` — honest push + transfer-stall heartbeat.** Two independent fixes bundled. The acked-bytes push needs `ItemDoneInfo` plumbing from `9fdf1f6` first. `SetTransferActive` is standalone and small.
3. **`9fdf1f6` — Apply/Retain `ItemDoneInfo`.** Real gap. Composition-root wiring on local needs the same 2-line addition origin's body documents. Verify against #019's PlanInfo-delta reset semantics (origin resets on PlanInfo; #019 anchors `Progress=100` on empty delta — these interact).
4. **R2 dotenv from `76c0682`.** Tooling-only, isolatable from the WIP bundle. Take just the dotenv pieces.

Skip:
- ETA caption from `4440fe8` — superseded by #018/#009/#017 dial telemetry.
- "Applying…" caption from `4440fe8` — Phase taxonomy (#017) intentionally collapses Pull+Apply; surface decision belongs to a #017 follow-up, not a back-port.
- Eventbus blocking subs from `76c0682` — needs livesync (#016) re-validation; risk > value at this stage.

## Trade-offs

| Decision | Cost | Benefit |
|----------|------|---------|
| Don't merge | `origin/feat/delta-sync` becomes a dead branch on the remote (or gets force-overwritten on next push) | History stays linear-ish; no broken merge commit; explicit per-feature back-port path |
| Force-push to overwrite origin | Anyone with the old origin tip checked out has to reset | Single source of truth; matches what the local timeline already represents |
| Defer back-ports to follow-up commits | Origin's perf + progress wins land later than they could | Each back-port can be validated in isolation against the relevant local design log; no all-or-nothing merge |
| Preserve `tmp-rebase-partial` branch | One extra local branch | Reflog safety net while back-ports happen |

## Verification

- This design log exists and indexes correctly.
- `feat/delta-sync` builds clean (`go build ./...` ok).
- `origin/feat/delta-sync` history is captured here (subject lines + bodies summarised) so a future back-port doesn't need to re-fetch context.
- A follow-up design log (or per-back-port commit messages) closes each item in the "Back-port candidates" list.

## Implementation Results

Code audit of HEAD `8c563f0` against the four back-port candidates:

| # | Candidate | Origin SHA | Status on local | Evidence |
|---|-----------|------------|-----------------|----------|
| 1 | Ref-attested live set perf | `4e010eb` | ✅ **Landed** | `refs/pull.go` `collectKnownHashes()` does one `dst.List("objects/")` pre-flight; `refs/transfer.go` `transferBlob` has no per-blob `Exists` gate; `push_test.go` `TestPusher_DoesNotCallExistsOnObjectsDuringTransfer` asserts it. Shipped via [[025-drop-redundant-exists-gate]]. |
| 4 | R2 dotenv | `76c0682` | ✅ **Landed** | `internal/config/dotenv.go` `LoadEnvFiles()` + `RITUAL_R2_*` mirror map. Shipped via [[030-r2-default-and-dotenv]]. |
| 2 | Honest push + stall heartbeat | `a7949ae` | 🟡 **Heartbeat half landed** | See below. Honest-push half still pending (depends on #3). |
| 3 | Apply/Retain `ItemDoneInfo` | `9fdf1f6` | ⬜ **Absent** | No `ItemDoneInfo` type; `apply.go` runs items with no per-item callback. Genuine gap. |

### Why the suite stayed green with #2/#3 absent

Apply tests assert file-placement correctness only (progress "delegated elsewhere" per the file header); push tests assert transfer correctness + the 025 Exists-gate removal, never *when* the bar hits 100% relative to `PutStream` ack; ticker tests cover idle-silence and the final zero-delta but never a mid-transfer stall; projection tests cover only Pulling/Pushing `Tick → BytesDone`, never Applying/Retaining and never that a Tick is the *only* driver of `BytesDone`. The frozen-bar and race-to-100% are composition-root timing symptoms no pure-function test asserts. Fix is to add behavioural regression tests alongside each back-port.

### #2 heartbeat — landed (this commit)

`internal/adapters/progress/ticker.go`:
- New `transferActive atomic.Bool` field + `SetTransferActive(bool)` method (mirrors origin's `Ticker.SetTransferActive`).
- `Run` gate now `if !active && !wasActive && !t.transferActive.Load() { continue }` — an explicitly-marked transfer that stalls (counters frozen) still pulses heartbeat ticks; idle stages leave the flag false and stay silent.

Red→green: `TestTicker_HeartbeatDuringActiveTransferStall` (deliberate inverse of `TestTicker_StableCounters_NoTicks`). Package `-race` clean; `StableCounters_NoTicks` and `OneFinalZeroDeltaAfterActivityStops` not regressed.

### #2 heartbeat — wired end-to-end (follow-up commit)

The heartbeat is now vertical, ticker → caption:

- **`internal/subsystems/transferwatch/`** (new) — `Watch` subscribes to the bus and calls `SetTransferActive(true)` on entering `StagePulling`/`StagePushing`, `false` on `pulling.ApplyStartedInfo` (apply is local) and every other transition. Brackets exactly the two byte-flowing windows so idle/local beats stay silent. `cmd/gui/main.go` starts `go transferwatch.New(bus, ticker).Run(ctx)`.
- **`projection`** — new `ViewModel.Stalled bool`; `onTick` sets it when `Stream.Instant==0 && BytesTotal>0 && BytesDone<BytesTotal` (a zero "now" rate with bytes still owed — *not* the trailing completion marker); cleared on every stage transition. Tests: `ZeroRateTickMidPush_MarksStalled`, `BytesResumeAfterStall_ClearsStalled`, `FinalZeroDeltaAtCompletion_NotStalled`, `StageChange_ClearsStalled`.
- **frontend** — `ritual-app.derive()` overrides the dial sub with "Stalled — waiting on R2…" when `vm.stalled`. New `StalledUpload` story walks ramp → stall → resume → tail.

Full Go suite green; `tsc --noEmit` clean; package `-race` clean on `transferwatch` + `progress`.

**Still pending for #2:** honest-push — drive `BytesDone` from acked `PutStream` returns rather than the compressor read-rate. Deferred behind #3's `ItemDoneInfo` plumbing (the acked-bytes signal *is* an `ItemDoneInfo` event). Tackled with #3.
