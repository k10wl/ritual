# 006 — Log Delivery Latency

Frontend log console lags minutes behind real events during file-heavy bursts. Cause: every event = one Wails IPC + one full Lit re-render of the log list. Fix: coalesce on the Go side, batch on the wire, append-only on the frontend.

## Background

Pipeline today (`cmd/gui/main.go:343`, `internal/gui/logsink/logsink.go`, `frontend/src/ritual-logs.ts`):

```
ports.EventBus ─► logsink.Sink ─► wailsLogEmitter ─► Wails IPC ─► onLog ─► RitualLogs.pushRow ─► Lit render
   (drop-on-full,   (1 ch,            (1 EmitEvent       (JSON +         (rows = next;
    bufLen=64)       serial Run)       per event)         postMessage)    full re-render)
```

- `adapters.NewEventBus(64)` is non-blocking — subscribers see drops once their 64-slot channel fills (`internal/adapters/eventbus.go:38`).
- `logsink.Run` drains one event at a time, synchronously calls `emitter.Emit` → `logsWindow.EmitEvent("log:line", line)`. One IPC roundtrip per event.
- `RitualLogs.pushRow` clones the 500-row ring on every entry and assigns to `@state rows`, triggering a full `<ol>` re-render (`ritual-logs.ts:64`).
- Stages publish 92+ distinct events; file ops (pull/apply/commit fan-out) burst hundreds per second.

`wailsViewEmitter` already solved the same problem for ViewModel snapshots (`cmd/gui/main.go:396-426`): latest-wins, single in-flight IPC, wake channel. Logs missed that pattern — every line goes through.

## Problem

Three coupled defects observed in the GUI logs window during file-heavy operations:

1. **Minutes of post-burst spam.** After a pull/apply burst finishes, the log console keeps appending entries long after the engine is idle. The drain rate is bounded by IPC + Lit re-render cost, not by event production.
2. **No coalescing.** Identical or near-identical events (per-file `acquiring.FileStarted`, decoder counters) emit one IPC each. There is nothing collapsing redundant ticks or merging same-tick siblings.
3. **Frontend amplifies the cost.** Each incoming line replaces the `rows` array (`this.rows = next`), so Lit diffs and re-renders up to 500 `<li>` elements per event. At 500 events/s this is unrenderable; the browser queues paints and the IPC backlog (Wails JS bridge + JS task queue) grows for minutes.

Symptom = whole system is one big unbounded queue. The on-disk log file is fine (file write is fast); only the **GUI delivery path** is bottlenecked.

## Questions and Answers

**Q1. Where should coalescing live — Go side, IPC layer, or frontend?**
A. **Go side, in the emitter.** Same pattern as `wailsViewEmitter`: a ring buffer + flusher goroutine. One IPC carries an array of N lines per tick (16ms default). Reasons: (a) IPC roundtrip dominates per-event cost — fewer roundtrips wins more than smaller payloads; (b) frontend stays oblivious to backpressure; (c) one place to drop / merge when the ring overflows.

**Q2. Drop, merge, or block on overflow?**
A. **Drop oldest, mark a gap.** Logs are diagnostic; blocking would stall the producer (violates `eventbus.go:18` policy). Merge needs message equality which most events don't have. Drop-oldest preserves the most recent context (what the user is staring at) and surfaces the gap as a single synthetic line `… dropped N log lines …` rather than silent loss.

**Q3. Batch size / interval — pick numbers.**
A. **Flush 16ms after the first event in an empty ring, OR immediately when the ring reaches 128 lines, whichever first.** Lazy timer — no ticker running at idle (any wake-cost without pending data is waste). 16ms = one 60Hz frame, the frontend can't paint faster anyway. 128 ≈ 60% headroom over typical 80-line/frame bursts and keeps one IPC payload bounded (~25KB JSON). Ring capacity 1024 (8 frames of slack before drop-oldest). All four are constructor args, not consts — see `feedback_config_on_struct`.

**Q4. Does the on-disk log file change?**
A. **No.** `internal/subsystems/logging` writes one line per event direct from the bus subscription. That path is fast and is the source of truth for postmortems. This design only touches the GUI delivery path.

**Q5. Frontend — keep the 500-row ring + Lit re-render?**
A. **Append-only DOM, per-frame batch.** Stop reassigning `this.rows`. Accept `LogLine[]` from the new `log:lines` event; on receive, push to a pending queue; on `requestAnimationFrame`, append created `<li>` nodes directly into the `<ol>` and trim the head past 500. Lit `@state` is the wrong shape for a write-heavy log buffer — it's a stream, not a model. (Reuse pattern: `wailsViewEmitter` is latest-wins because ViewModel is a model; logs are a stream.)

**Q6. New event name or reuse `log:line`?**
A. **New event: `log:lines` carrying `{lines: LogLine[], dropped: number}`.** Old `log:line` deletion is part of this change — no backwards compat per `project_no_backwards_compat`.

**Q7. Does drop-oldest interact with the existing bus drop?**
A. **Layered drop is fine.** Bus drops first when the logsink channel fills (64). Emitter ring drops oldest when frontend can't drain (1024). Each layer logs its drop count to a synthetic line in the next batch. Two drop points are honest: bus drop = "logsink fell behind"; emitter drop = "frontend fell behind". Distinct diagnostics.

**Q8. Where does the gap synthetic line come from?**
A. **The emitter generates it.** When the ring drops K entries between two successful flushes, the next batch prepends `LogLine{Level: warn, Msg: "log delivery dropped N lines"}`. logsink itself stays pure (1:1 event→LogLine).

## Design

```
ports.EventBus ─► logsink.Sink ─► batchingLogEmitter (ring) ─► Wails IPC (1 EmitEvent / 16ms) ─► onLogs ─► RitualLogs (rAF append)
   (drop@64)                       (drop oldest@1024,
                                    gap line on drop)
```

### Go: `wailsLogEmitter` becomes `batchingLogEmitter`

Mirror `wailsViewEmitter` shape — same authorship pattern, same review burden:

Lazy timer — at idle the goroutine parks on `wake`, zero ticks fire. The first `Emit` after an empty ring schedules a single deadline; subsequent `Emit`s inside that window coalesce into the same flush. If the ring crosses `BatchMax` before the deadline, the size trigger preempts the timer.

```go
type batchingLogEmitter struct {
    win     atomic.Pointer[application.WebviewWindow]
    mu      sync.Mutex
    ring    []logsink.LogLine // cap = 1024, FIFO
    dropped int
    wake    chan struct{}     // buffered, cap 1
    cfg     batchCfg
}

type batchCfg struct {
    Capacity int           // ring size (1024)
    BatchMax int           // size trigger — flush immediately at this many lines (128)
    Interval time.Duration // coalescing window after first event in empty ring (16ms)
}

func (e *batchingLogEmitter) Emit(line logsink.LogLine) {
    e.mu.Lock()
    if len(e.ring) == e.cfg.Capacity {
        e.ring = e.ring[1:] // O(N) — acceptable at 1024; switch to head index if hot
        e.dropped++
    }
    e.ring = append(e.ring, line)
    sizeTrigger := len(e.ring) >= e.cfg.BatchMax
    e.mu.Unlock()
    if sizeTrigger {
        // non-blocking nudge; loop reads wake and flushes without waiting on the deadline
        select { case e.wake <- struct{}{}: default: }
    }
}

func (e *batchingLogEmitter) loop(ctx context.Context) {
    for {
        // Park until something to flush. No ticker — idle = zero wakeups.
        select {
        case <-ctx.Done(): return
        case <-e.wake:
        }
        // Coalesce: wait up to Interval for more lines or a size trigger.
        timer := time.NewTimer(e.cfg.Interval)
        select {
        case <-ctx.Done(): timer.Stop(); return
        case <-e.wake: timer.Stop() // size trigger fired — flush now
        case <-timer.C:
        }
        e.flush()
    }
}

func (e *batchingLogEmitter) flush() {
    e.mu.Lock()
    if len(e.ring) == 0 && e.dropped == 0 { e.mu.Unlock(); return }
    n := min(len(e.ring), e.cfg.BatchMax)
    lines := append([]logsink.LogLine(nil), e.ring[:n]...)
    e.ring = e.ring[n:]
    dropped := e.dropped
    e.dropped = 0
    leftover := len(e.ring) > 0
    e.mu.Unlock()

    w := e.win.Load()
    if w != nil {
        w.EmitEvent("log:lines", logsink.LogBatch{Lines: lines, Dropped: dropped})
    }
    // If BatchMax capped this flush, re-arm immediately so the leftover doesn't wait idle.
    if leftover {
        select { case e.wake <- struct{}{}: default: }
    }
}
```

The `Emit` path stays cheap: one lock, one slice op, one non-blocking channel send only when the size trigger crosses. Every other `Emit` inside an active coalescing window just appends — the deadline already running drains them on schedule.

`logsink.Sink` is untouched — it still calls `emitter.Emit(line)` per event. The batching lives behind the `Emitter` interface; `logsink_test.go` uses a slice accumulator and stays valid.

### Frontend: stream-shaped, not model-shaped

```ts
@customElement("ritual-logs")
export class RitualLogs extends LitElement {
    private rowEl!: HTMLOListElement;  // direct DOM, not Lit-managed children
    private pending: Row[] = [];
    private flushScheduled = false;

    connectedCallback() {
        super.connectedCallback();
        this.unsubscribe = onLogs(batch => {
            if (batch.dropped > 0) this.pending.push(gapRow(batch.dropped));
            for (const line of batch.lines) this.pending.push(toRow(line));
            this.scheduleFlush();
        });
    }

    private scheduleFlush() {
        if (this.flushScheduled) return;
        this.flushScheduled = true;
        requestAnimationFrame(() => {
            this.flushScheduled = false;
            this.drainPending();
        });
    }

    private drainPending() {
        const frag = document.createDocumentFragment();
        for (const r of this.pending) frag.appendChild(renderRow(r));
        this.pending.length = 0;
        this.rowEl.appendChild(frag);
        while (this.rowEl.childElementCount > RING_CAPACITY) this.rowEl.firstElementChild!.remove();
        this.rowEl.parentElement!.scrollTop = this.rowEl.parentElement!.scrollHeight;
    }
}
```

`render()` returns the static shell (`<header><div class="wrap"><ol></ol></div><footer>…</footer>`); rows are imperatively appended into `<ol>`. Lit still owns the editor footer and the empty-state placeholder.

### Why not just enlarge bus buffer / use blocking publish

- Enlarging the per-subscriber buffer hides the problem, doesn't fix IPC cost.
- Blocking publish would stall the engine on a slow GUI (explicitly disallowed by `eventbus.go:18`).
- The cost is in IPC roundtrips and DOM work, not channel mechanics.

## Implementation Plan

1. **`internal/gui/logsink`**: add `LogBatch{Lines []LogLine, Dropped int}` type. No changes to `Sink`.
2. **`cmd/gui/main.go`**: rename `wailsLogEmitter` → `batchingLogEmitter`, add ring + `loop` goroutine, register `log:lines` instead of `log:line`. Start the loop in `guiRuntime.start`, stop it in shutdown.
3. **`frontend/bindings`**: regen Wails bindings (new event type).
4. **`frontend/src/wails-api.ts`**: replace `onLog` with `onLogs(handler: (batch: LogBatch) => void)`. Delete the per-line path.
5. **`frontend/src/ritual-logs.ts`**: switch to imperative `<ol>` append + rAF batching; drop the `@state rows` array; keep the 500-row trim.
6. **Tests**:
   - `cmd/gui/logemitter_test.go` (new): burst of 10_000 events at 0-interval → exactly `ceil(10000/128)` flushes, no drops, all lines present in order.
   - Overflow: pre-fill 2048 events with a paused window (nil emitter), then bind → first batch carries a gap line with `Dropped > 0`, subsequent batches have `Dropped = 0`.
   - **Idle quiescence**: run the emitter for 1s with no `Emit` calls; assert zero `EmitEvent` invocations on the test window AND zero goroutine wakeups visible via a wrapped `wake` channel counter. This is the load-bearing assertion for the lazy-timer design.
   - `logsink_test.go` stays green (emitter is an interface; per-line semantics unchanged).
7. **Friction-to-test** (`feedback_friction_to_test`): the "logs spam for minutes after burst" symptom must become an integration test — drive 5000 publishes through the real bus + sink + emitter, assert flush completes within 200ms after publishes stop and that the test-side emitter saw a bounded number of batches.

## Trade-offs

- **Lost per-line ordering across drops.** When the emitter drops oldest, the gap line marks the boundary but the dropped lines themselves are gone from the GUI. The on-disk log retains them — operators inspecting a postmortem still see everything. Acceptable for a diagnostic surface.
- **16ms latency floor.** A single event now waits up to one frame before reaching the UI. Imperceptible interactively; this is the entire point of the change.
- **DOM imperative-in-Lit.** Mixing direct DOM mutation with Lit's reactive model is unusual. Justified because the row list is a high-frequency stream, not a model state; Lit's diff has nothing to optimise for append-only.
- **Ring uses slice-shift on overflow.** O(N) at 1024 is fine; switch to head-index ring if profiling says otherwise (`feedback_shape_first_then_stdlib`).

## Verification

1. **Burst test.** Trigger a fresh pull on a 5GB world. The log console must catch up to "Idle" within ≤ 1s of the engine's `StatusChanged{Idle}` event. Today: minutes.
2. **Flush cadence.** Wire a counter to `EmitEvent("log:lines", …)`; observe ≤ 65 IPC calls/s under continuous publish at 5000 events/s (vs current 5000 calls/s).
3. **Gap visibility.** Force overflow (1024 lines while window is hidden); the first visible batch shows exactly one `… dropped N log lines …` line, N ≥ 1.
4. **No drops at production rates.** With a typical 500 events/s burst sustained 5s, expect `Dropped == 0` across all batches.
5. **On-disk log unchanged.** Diff `<root>/logs/<ts>.log` before/after the change for an identical fixture run; line count and content must match.

## Open

- Should the gap synthetic line carry a structured payload (level=warn, dropped=N) so the frontend can style it distinctly? Current sketch encodes only via message text. Lean: yes, add `Dropped` as a sibling field on the batch, render a dedicated `<li class="row-gap">` so it's visually distinct from log-level warnings.
