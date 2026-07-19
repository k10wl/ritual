# 051 — GUI freeze on save: wailsViewEmitter loses the terminal Idle snapshot

**Date:** 2026-07-19
**Status:** Implemented and confirmed live
**Related:** [[026-stuck-in-saving]] (original hypothesis-driven investigation, now superseded by the confirmed findings below — 026 is left unedited per immutability rule), [[050-chained-launch-progress-overflow-and-missing-icons]] (adjacent recent work on the same `projection.go`/`cmd/gui/main.go` code), [[006-log-delivery-latency]]/[[042-server-logs-console]] (the `batchingLogEmitter` sibling that shares the same emitter shape but is a separate object/goroutine).

## Background

User reported the GUI dial getting permanently stuck on a mid-flow phase (e.g. "Saving") after a Push/Upload completes, never returning to Idle, with no way to recover short of restarting the app. [[026-stuck-in-saving]] investigated this on 2026-05-25 but could not confirm a root cause — the leading hypothesis at the time was `rune-decoder` `.text` reactivity (hypothesis C), later downgraded after the user reported one instance self-resolving after several minutes (hypothesis shifted to delayed, not lost, delivery — Q12).

This log picks up from a **live repro caught in the act** on 2026-07-19, using the user's own session log (`~/k10wl/ritualdev/logs/20260719122428.log`) plus a DevTools inspection of the still-frozen window.

## Problem

Same user-facing symptom as 026, now with hard evidence pinpointing the failure to a single function, and two of 026's live hypotheses (rune-decoder reactivity, and — newly proposed this session — per-file-volume overwhelming the frontend) definitively ruled out.

## Questions and Answers

**Q1.** What did the live repro show?
**A.** Session `20260719122428.log` ran an Upload/Publish flow ending cleanly:
```
[12:31:29] Unlocking → Done
[12:31:29] status: done
[12:31:29] [snap] stage=uploading phase=saving done=1172307415/1280884251 speed=1125.78Mbps   ← stale, re-fired on the Done state-change fold
[12:31:29] [snap] stage=idle phase=idle done=0/0                                              ← correct terminal snap
```
No further log activity for 26+ minutes. The GUI window, screenshotted live, still showed "Saving", 1.09 GB / 1.19 GB, 190.6 MB/s — numbers matching the *stale* pre-Done snap byte-for-byte.

**Q2.** Is this a frontend rendering bug (026's original hypothesis C)?
**A.** No — ruled out. DevTools console on the frozen window: `document.querySelector('ritual-app').vm` returned a ViewModel identical to the stale snap (`bytesDone: 1172307415, bytesTotal: 1280884251, speedMbps: 1125.7812352`), not the idle one. The Lit component's own `@state vm` field never changed, so there was nothing for Lit/`<rune-decoder>` to fail to repaint. The correct terminal `ViewModel` never arrived at the frontend at all.

**Q3.** Is this a bus-drop (026 hypothesis A) or a projection-logic bug?
**A.** No — ruled out. The same terminal `Snap{idle}` event that should have reached the frontend also flows to `internal/subsystems/logging`'s bus subscriber (an independent subscription off the same bus), and that subscriber's output — the `[snap] stage=idle phase=idle` line — **is present** in the file log. Since both subscribers are fed from the same `Projection.Run` loop in program order (`p.emitter.Emit(p.state)` then `p.publish(Snap{p.state})`, `internal/gui/projection/projection.go:142-145`), the divergence must be downstream of the projection/bus — specifically inside `wailsViewEmitter` or the Wails/WebView2 IPC call it makes.

**Q4.** So where exactly?
**A.** `cmd/gui/main.go:737-774`, `wailsViewEmitter`. It's a single-worker "latest-wins" coalescing dispatcher: `Emit()` does `pending.Store(&vm)` + a non-blocking `wake<-` signal; a single dedicated goroutine (`loop()`) does `for range e.wake { vm := pending.Swap(nil); a.Event.Emit("ritual:view", *vm) }`. This design correctly collapses bursts (only the latest pending VM survives between drains) — but it has **no timeout, no error handling, and no recovery around `a.Event.Emit`**. If that one native call ever blocks or hangs, the `loop()` goroutine never returns to `for range e.wake`, so every subsequent `Emit()` call still succeeds at the `pending.Store`/`wake` level (they're just atomic/buffered-chan ops) but is never drained again — for the rest of the process's life. That matches the observed shape exactly: not "eventually caught up" (026 Q12's case) but "frozen forever, zero further attempts," because this specific instance never recovered even after 26+ minutes.

**Q5.** User's new hypothesis: an 814-file Push generates enough per-file update volume that the frontend gets "bombarded" and needs throttling. Does the per-file volume reach the frontend event stream?
**A.** No — **falsified** by direct code read. Per-file storage events (`storage.putstream`/`storage.exists`/etc. — the bulk of this session's 19,394-line/2.4MB log) are bus-published and printed by the file-log subscriber, but `Projection.fold()` (`internal/gui/projection/projection.go:152-228`) has no case for them; they fall through to `default → foldUpdate → return false` (no emit). The only events that ever reach `p.emitter.Emit` are `progress.Tick` — hard-capped at 1/s by `time.NewTicker(time.Second)` (`Ticker.Run`, `internal/adapters/progress/ticker.go:121`, constructed at `cmd/gui/main.go:655`) — and a handful of coarse stage/status transitions (roughly a dozen per whole session). `ritual:view` emission is already rate-limited to ~1/s by construction, independent of file count. Throttling that stream further would not have prevented this repro, and doesn't fit the failure shape anyway (a single terminal call going silent forever, not gradual backpressure).

**Q6.** Why would `a.Event.Emit` block or silently swallow a call in the first place?
**A.** Unconfirmed — open question. This is the one hop outside this repo's own code (Wails v3 core / WebView2 native IPC bridge). Two untested angles: (a) temporary instrumentation wrapping the call with a timer + `recover()` to catch it live next time; (b) check upstream Wails v3 issues for known `Event.Emit` stalls under WebView2 (window minimize/occlusion, native message-pump starvation, etc.).

**Q7.** Is `wailsViewEmitter`'s sibling, `batchingLogEmitter` (server console log batching, [[042]]), at risk of the same wedge?
**A.** Structurally similar (single loop goroutine draining a wake channel), but it calls `w.EmitEvent(...)` on the **logs window**, not the main window, and its own blocking-call risk hasn't been checked yet. Worth a quick look when fixing this, since the same defensive pattern (timeout/health-check around the IPC call) would likely apply to both.

**Q8.** Second live repro, 2026-07-19, session `20260719161049.log` (via [[053-cdp-webview-inspection]], now working end to end). Is the freeze a general IPC/WebView2 stall, or specific to the push-event channel?
**A.** **Specific to the push-event channel — confirmed directly, not just inferred.** Log shows the identical terminal race (`Unlocking → Done` → `status: done` → stale `[snap] phase=saving done=684501645/1280884251 speed=50.83Mbps` → correct `[snap] phase=idle`), silent since `16:12:10`. With the window still live and frozen, CDP confirmed two things in the same sitting:
  1. `document.querySelector('ritual-app').vm` read back the exact same stale numbers as the log's pre-Done snap — 4th instance of this exact byte-identical match (after the 12:31:29 repro's 3 matches).
  2. **New probe**: from the same frozen page, dynamically importing the real `wails-api.ts` module (as served by Vite dev, `http://wails.localhost:9245/src/wails-api.ts`) and calling the real `getSnapshot()` export — i.e. `Control.GetSnapshot()` over the Wails **Call/RPC** transport, a completely different code path from `wailsViewEmitter`'s **Event/push** transport — returned in **6ms** with the correct `{phase: "idle", ...}`.
  This rules out a general WebView2/IPC-bridge stall: the same native bridge, on the same frozen window, at the same moment, answers a request/response call instantly and correctly. Only the one-way push channel (`wailsViewEmitter.loop()` → `a.Event.Emit`) is dead. This narrows Q6 from "somewhere in the Wails/WebView2 boundary" to specifically **the `Event.Emit` push path**, not anything shared with `Call`/RPC — a fix can target that one call in isolation with high confidence it won't need to touch or account for the Call transport at all. **Correction in Q9: "proves the native bridge is healthy" overclaims — `GetSnapshot()` never touches the WebView2-bound UI thread at all, so it can't prove that thread is unstuck. Refined mechanism below.**

**Q9.** Traced the actual Wails v3 source (vendored `pkg/application`) for what `Event.Emit` does end to end, to find where in that chain a hang is even possible.
**A.** `EventManager.dispatch()` (`event_manager.go:128-140`) loops **sequentially, synchronously, with no timeout or per-listener isolation**, over every registered `wailsEventListeners` entry (every `WebviewWindow` ever created — a single un-sharded list, not per-event-name). For each: `WebviewWindow.DispatchWailsEvent` → `ExecJS` → `InvokeSync` (`mainthread.go:23`) — which posts a closure to **one single dedicated Windows OS thread** running a classic `GetMessage`/`DispatchMessage` loop (`mainthread_windows.go`, `runMainLoop`/`dispatchOnMainThread`) and **blocks the calling goroutine on a `sync.WaitGroup` until that closure actually runs on that thread**. The closure then calls `windowsWebviewWindow.execJS` → `chromium.Eval` (vendored `go-webview2@v1.0.23/pkg/edge/chromium.go:277`) → `webview.ExecuteScript(script, nil)` — a fire-and-forget COM submission into the separate WebView2 browser process (nil completion callback, so Wails itself doesn't wait on the *result*, only on the *submission*).
  Consequence: `GetSnapshot()` (Q8) is pure Go (`projection.Snapshot()`, a struct copy) and never touches this dedicated UI thread or any WebView2 API — its success proves the backend and the Call/RPC dispatch path are healthy, but says **nothing** about whether the one dedicated UI thread is stuck. The full evidence (frozen dial, correct RPC, dead push events, non-self-recovering) is fully consistent with a sharper hypothesis: **that single dedicated Windows UI thread is itself wedged inside one `ExecuteScript` submission call**, and since every future `Event.Emit` (for any window, any event) needs that same thread, the wedge is permanent and total for the push channel specifically — while everything that doesn't need that thread (RPC, and apparently WebView2's own content rendering, which runs in a separate process) stays completely unaffected. This also explains why the frozen window still visually renders correctly and CDP can still attach and evaluate JS: CDP talks directly to the WebView2 renderer process, never touching our app's stuck host-side UI thread.
  Checked whether this specifically requires a *second* registered window (e.g. the lazy [[043]] logs window sitting hidden, jamming the loop ahead of the main window's turn): both repros' `GET /json/list` CDP output showed **only one** registered window/target each time — ruled out. The sole main window's own `ExecuteScript` submission is what hangs; no second window is required to reproduce.
  **Floor of what source-reading can answer**: *why* one `ExecuteScript` submission into WebView2/COM can block forever is internal to the WebView2 runtime, not visible from Wails' Go source. Next step to get a real (not inferred) answer: `dlv attach <pid>` on a live frozen process next repro, to pull a full goroutine dump — would show definitively whether the dedicated UI thread is stuck inside the `ExecuteScript` call (confirms) or elsewhere (redirects the whole hypothesis). `dlv` v1.25.2 confirmed installed and available on this machine. Not yet attempted — needs a live repro to attach to.

**Q10.** *Third live repro, 2026-07-19, via [[053]] + Delve.* Attached `dlv` headlessly (`--continue`, non-disruptive) to the live `ritualdev.exe` (PID 18464) **before** reproducing, so the freeze could be caught the instant it happened, exactly as it happens. On freeze: `halt` + `goroutines -t 15` dumped **all 26 goroutines** in the process. Result — **directly refutes the Q9 hypothesis**:
  - Every single goroutine is idle/parked (GC housekeeping, or blocked in a channel `select`/`chanrecv` waiting for the *next* item). Critically, `main.(*wailsViewEmitter).loop` (goroutine 8) is back at its `for range e.wake` receive point — meaning its last call all the way through `Emit → ExecJS → InvokeSync → execJS → chromium.Eval → ExecuteScript(script, nil)` **already returned**. Nothing is hung. No goroutine anywhere is inside `ExecuteScript`, `InvokeSync`'s `wg.Wait()`, or any cgo call related to it.
  - So the entire Go-side call chain completes normally every time — the wedge is not a Go deadlock at all.
  - **Decisive follow-up test, same live freeze, before the app was closed**: manually invoked the *exact* function the native code calls — `window._wails.dispatchWailsEvent({name: "ritual:view", data: <fabricated idle vm>})` — directly via CDP `page.evaluate`, bypassing Go entirely. Result: **the frozen dial fixed itself instantly** (`vm` flipped from the stale saving snapshot to correct idle the moment the call ran). This proves the JS-side listener registration, `eventListeners` map, and Lit reactivity are all completely healthy — nothing wrong on that side either.
  - **Conclusion**: the gap sits precisely between two independently-proven-healthy points. `ExecuteScript(script, nil)` (`go-webview2@v1.0.23/pkg/edge/chromium.go:277`) is a **fire-and-forget COM call** — WebView2 accepts the submission (Go sees no error, `Eval` returns normally) but the actual script silently never runs in the renderer, and *nothing anywhere is told* — not a Go error, not a JS error, not a log line. It's not a deadlock, not a timeout-shaped bug, not a frontend bug: it's a genuine reliability gap in an unconfirmed, unacknowledged, fire-and-forget delivery mechanism used for a critical state transition.
  - **The gap is fixable, not just diagnosable**: the underlying WebView2 binding *does* support a real completion callback — `ICoreWebView2.ExecuteScript(javascript string, handler *iCoreWebView2ExecuteScriptCompletedHandler) error` (`go-webview2@v1.0.23/pkg/edge/corewebview2.go:295`) is a full COM completion-handler interface, generated vtable and all. `Chromium.Eval` simply hardcodes `nil` for it, discarding the one piece of information that would surface this exact failure. This isn't a WebView2 limitation; it's a wrapper-layer simplification that trades away delivery confirmation.
  - Process exited cleanly (status 0, user-initiated) immediately after these two tests — both results were already captured beforehand; nothing lost.

**Q11.** *Fourth live repro, 2026-07-19, session `20260719173135.log` + a full browser-console capture from the permanent IN/OUT echo instrumentation ([[053]]).* With both a Go-side log (via the `FAILEDHERE` markers already wired into `wailsViewEmitter.loop`) and a browser-side console log of every event actually reaching the JS engine for the *same* session, the exact terminal sequence can be matched byte-for-byte on both sides. Does this confirm or refine Q10's "silently never runs" conclusion?
**A.** **Refines it — the message is not silently lost, it's delivered out of order, and directly proven this time (not inferred).** Backend log, in strict submission order (all confirmed returned via `FAILEDHERE-after`):
```
1. saving done=331103486  (17:32:38)
2. saving done=331103486  (17:32:39, dup)
3. saving done=331103486  (17:32:39, dup)
4. saving done=331103486  (17:32:39, dup — Unlocking→Done state-change fold)
5. idle                    (17:32:39 — submitted LAST)
```
Browser console, same five events, same counts, different arrival order:
```
1. saving done=331103486  (14:32:38.245Z)
2. saving done=331103486  (14:32:39.192Z)
3. saving done=331103486  (14:32:39.206Z)
4. idle                    (14:32:39.207Z)
5. saving done=331103486  (14:32:39.213Z)   ← arrives AFTER idle, though Go submitted it BEFORE idle
```
Emit #4 (a stale "saving" duplicate) was submitted to WebView2 *before* emit #5 (idle), but *executed* in the renderer *after* idle. `ritual-app.ts`'s `applyVm` does an unconditional `this.vm = vm` with no ordering guard, so whichever script *finishes executing* last wins the frontend's displayed state — regardless of which was *sent* last. That's why the dial froze on stale "saving" data: not because idle vanished, but because a stale duplicate raced it and won.
This means `ExecuteScript`'s fire-and-forget calls do not preserve submission order as execution order under load (multiple back-to-back submissions to a busy renderer) — a distinct, more precise mechanism than Q10's "silently never runs," though consistent with everything else already found (nothing hangs, both channels are independently healthy, the gap is specifically in this delivery path).
**Fix implication**: an ack/retry scheme (Q10's design sketch, option B) would still work but is now overkill for what's actually a **causal-ordering** problem, not a **loss** problem. The precise, minimal fix: attach a monotonically increasing sequence number to every emitted `ViewModel` (or reuse the existing per-emit counter shape) and have `applyVm` **drop any incoming snapshot whose sequence number is not greater than the last-applied one**. This defeats out-of-order execution regardless of why WebView2 reorders it, needs no acknowledgment/round-trip, and is a standard, well-understood technique (the same idea as TCP sequence numbers or a Lamport clock) for exactly this class of problem.

## Design

**Superseded by Q10's direct evidence**: the original two candidate directions below (drafted before the live Delve/CDP tests) assumed the native call *blocks* — Q10 proves it doesn't; `ExecuteScript` always returns, the submitted script just sometimes never runs, silently. A timeout/watchdog around a call that already returns fine would never fire — it doesn't address the actual gap. Left here for the record, not as the plan:

1. ~~Guard the emit call with a timeout + watchdog.~~ Moot — there is nothing to time out.
2. ~~Add a redundant/independent recovery path (periodic health-check emit).~~ Superseded by a more targeted option below that reuses the *already-proven-reliable* Call/RPC channel instead of adding a generic poll.

**Superseded again by Q11's direct evidence**: (A)/(B) below were designed against Q10's "silently never runs" framing (a *loss* problem). Q11 proves the real shape is *out-of-order execution* (nothing is lost — a stale duplicate just finishes executing after the correct final one and overwrites it). (A)/(B) would still work (both make the frontend correctly ignore/never-receive the stale straggler) but are more machinery than the problem needs. Left for the record:

- ~~(A) Wire real completion confirmation into the existing call.~~ Would work (a completed-in-order confirmation naturally prevents the stale duplicate from being treated as newer) but touches a third-party dependency.
- ~~(B) Application-level acknowledgment over the Call/RPC channel.~~ Would work but adds a round-trip per emit for a problem that doesn't require one.

**Real design, informed by Q11**: this is a causal-ordering problem, not a loss problem — fix it with ordering, not acknowledgment. Attach a monotonically increasing sequence number to `ViewModel` (incremented once per `p.emitter.Emit(...)` call in `Projection.Run`, `internal/gui/projection/projection.go:119-148`), carried through unchanged by `wailsViewEmitter`. Frontend's `applyVm` (`ritual-app.ts:251`) drops any incoming snapshot whose sequence number is not strictly greater than the last-applied one, before doing anything else with it. No round-trip, no acknowledgment, no timer — purely a local, synchronous comparison on receipt. Same idea as TCP sequence numbers / a Lamport clock: it doesn't stop messages from arriving out of order, it just makes stale ones inert on arrival.

## Implementation Plan

Not started. Proposed:
1. Add a `Seq int64` field to `projection.ViewModel`; `Projection` increments a counter and stamps it on every emitted snapshot (`Run`, wherever `p.emitter.Emit(p.state)` is called).
2. `ritual-app.ts`'s `applyVm` tracks the last-applied `Seq`; if an incoming `vm.seq <= lastSeq`, return immediately without touching `this.vm`.
3. Regression test: a projection/ViewModel-level test asserting sequence numbers are strictly increasing across a session, plus a frontend test feeding `applyVm` an out-of-order pair (high seq then low seq) and asserting the low one is ignored.
4. Remove the `FAILEDHERE`/IN-OUT-echo temp framing question — the permanent echo logging (053) stays; only the `FAILEDHERE` markers themselves (tagged `// REMOVE AFTER 051`) get removed once this fix is verified.

## Verification

Same as [[026-stuck-in-saving]]'s original criteria: after a full successful session ending in `status: done`, the dial visibly returns to idle (label "Start", glyph play, arc 0, no leftover phase text) and a second session immediately after starts cleanly. Additionally: the fix should survive an artificially-forced slow/blocked `a.Event.Emit` (once Q6 is answered, this becomes a targeted regression test) without permanently wedging the emitter.

## Trade-offs

- **Q6 is still open.** Any fix designed before confirming why the native call stalls risks treating a symptom (add a timeout) without addressing the mechanism — acceptable as a defensive measure regardless, but not a substitute for understanding the actual stall. (Superseded by Q11 anyway — see below; Q6's "why does it stall" no longer applies since Q11 shows it doesn't stall, it races.)
- **This log intentionally does not touch [[026-stuck-in-saving]].** Per project convention, existing design logs are immutable once written; 026 remains as the historical record of the original (partially wrong) hypothesis trail, and this log supersedes its conclusions.

## Implementation Results

Implemented 2026-07-19, per the Seq-guard design (Q11), user-approved ("seq approved").

**Go (`internal/gui/projection/`):**
- `viewmodel.go`: added `Seq int64 \`json:"seq"\`` to `ViewModel`.
- `projection.go`: added `nextSeq int64` to `Projection`; the three separate `p.emitter.Emit(p.state); p.publish(Snap{p.state})` call sites in `Run` (initial emit, 1Hz uptime tick, fold-relevant event) were consolidated into one `p.emit()` method that increments `nextSeq`, stamps it onto `p.state.Seq`, and dispatches — the single call site is what guarantees uniqueness/monotonicity.
- **Deviation from plan, caught by user mid-implementation**: adding `nextSeq`/`Seq` surfaced a **pre-existing, unrelated data race** — `Snapshot()` (called by `ControlService.GetSnapshot()` from whatever goroutine services that RPC) read `p.state` with zero synchronization against `Run`'s single mutating goroutine, for every field, not just the new one. `go test -race` isn't usable in this environment (the local C toolchain doesn't support 64-bit mode), so this was traced by hand instead of empirically confirmed. Fixed properly rather than papered over: added `sync.RWMutex` (`Projection.mu`) — `Snapshot()` takes `RLock`, `emit()` takes `Lock` around the stamp-and-copy (releasing before the potentially-slow `Emit`/`publish` calls), and `Run`'s two mutation sites (`fold(evt)`, `UptimeSeconds` write) are wrapped in the same lock. Single-writer/many-reader, not mutual exclusion between writers — `Run` was always the only mutator.

**Frontend:**
- New `frontend/src/vm-seq.ts`: `isNewerSnapshot(current, incoming)` — a tiny, dependency-free pure predicate (`incoming.seq > current.seq`), extracted into its own module specifically so it can be unit-tested without importing `ritual-app.ts` (see test deviation below).
- `ritual-app.ts`'s `applyVm` calls `isNewerSnapshot` as its first line; on a stale/out-of-order/duplicate snapshot it **warns and drops** — `console.warn` with both the incoming and currently-applied seq/stage/phase — per user direction ("instead of return do warn"): the drop must be observable, not silent, given the whole point of this session's tooling was making previously-invisible behavior visible.
- `FALLBACK_VM` (the pre-first-Emit placeholder) explicitly sets `seq: -1` (not left at the implicit zero-value default) — guards a narrow startup race where `GetSnapshot()` could return the backend's own zero-value state (`Seq` not yet stamped, if called before `Projection.Run`'s first `emit()`) with `Seq: 0`, which would have tied with a naive zero-default placeholder and wrongly failed the `>` check.
- Bindings regenerated (`wails3 generate bindings`) — `seq` appears correctly in the generated `ViewModel` TS class.

**Test deviation**: the plan called for a frontend test driving `applyVm` through a mounted `<ritual-app>`. Blocked by an unrelated, pre-existing infra gap — importing `ritual-app.ts` at all (even without mounting) transitively pulls in `ritual-dial.ts` → `svgpath`, which has a CJS/ESM interop error under this project's `@web/test-runner` module resolution; no existing test file happens to import `ritual-app.ts` directly, so nothing had surfaced this before. Out of scope to fix here. Resolved by testing `vm-seq.ts`'s `isNewerSnapshot` directly (new `vm-seq.test.ts`, zero imports beyond the function itself) instead of through the mounted component — the same logic, verified in isolation.

**Test results**: Go — new `TestProjection_Seq_StrictlyIncreasingAcrossEveryEmit` (asserts strictly increasing `Seq` across a mixed tick/state-change/status sequence, including the exact "duplicate state-change with no visible field change" shape from the real repro) passes; full repo `go test ./...` passes. Frontend — new `vm-seq.test.ts` (4 cases: strictly-greater accepted, equal rejected, lesser rejected using the exact captured repro's seq 6-then-5 shape, and the `-1` placeholder-vs-zero startup edge case) passes; full frontend suite 182/182 passes. `npx tsc --noEmit` clean throughout.

**Live confirmation, 2026-07-19, five consecutive real Upload cycles in one dev session** (console dump via [[053]]'s permanent IN echo). The race is still happening exactly as before — several sequence numbers (20, 37, 53, ...) never arrive in the browser at all across different cycles, confirming the underlying WebView2 delivery unreliability is unchanged — but it's now inert. The fifth cycle caught the exact race live:
```
ritual:view {seq: 123, stage: 'idle', phase: 'idle', ...}          ← arrives first
ritual:view {seq: 122, stage: 'uploading', phase: 'saving', ...}   ← stale straggler, arrives AFTER idle
[applyVm] dropped stale/out-of-order snapshot: incoming seq=122 (uploading/saving) <= applied seq=123 (idle/idle)
```
The dial stayed on idle; no freeze. This is the same race pattern as Q11's original capture (a stale duplicate executing after the terminal idle), caught live with the fix in place, doing exactly what it was designed to do. Verification criteria met.

**Remaining cleanup**: the `FAILEDHERE` markers (`cmd/gui/main.go`, tagged `// REMOVE AFTER 051`) can now be removed — their job (confirming the Go-side emit call always returns) is superseded by the permanent IN/OUT echo from [[053]], which is more informative and stays.
