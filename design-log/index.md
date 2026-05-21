# Design Log Index

Catalogue of design logs in order. New entries append to the bottom.

**Status values:** `Draft` · `Approved` · `In Progress` · `Implemented` · `Superseded`

| #   | Title                                             | Date       | Status      | Description                                                                          |
|-----|---------------------------------------------------|------------|-------------|--------------------------------------------------------------------------------------|
| 001 | [Progress Projection](001-progress-projection.md) | 2026-05-17 | Implemented | Two counter layers (logical / wire), three speed flavours, projection as pure picker. |
| 002 | [GUI Reset](002-gui-reset.md)                     | 2026-05-17 | Draft       | Browser CSS reset + tokens + bundled Inter + shared primitives; kill cross-platform drift. |
| 003 | [Encoder Pool](003-encoder-pool.md)               | 2026-05-18 | Implemented | Replace single zstd encoder + mutex with sync.Pool mirror of decoder side; unblocks pull fan-out. |
| 004 | [R2 Retry Decorator](004-r2-retry-decorator.md)   | 2026-05-18 | Implemented | RetryingStorage decorator + RangeGetter capability for mid-stream resume; fix GetStream error attribution. |
| 005 | [Storybook Harness](005-storybook-harness.md)     | 2026-05-20 | Implemented | Storybook (web-components-vite) reuses real `wails-api` via `setTransport` interception; lifecycle driven by app buttons. |
| 006 | [Log Delivery Latency](006-log-delivery-latency.md) | 2026-05-20 | Draft     | Coalesce GUI log events on Go side (ring + 16ms flush) + rAF append on frontend; kill minutes-long post-burst spam. |
| 007 | [HIG UX Coherence: one dial](007-hig-ux-coherence.md) | 2026-05-20 | In Progress | Steam × macOS Software Update metaphor: single morphing circular dial replaces five stage layouts; IDLE→PREP→RUN→FINAL loop with color tokens; LOCKED+FAILED as overlays; no banners; settings behind ⚙ gear. |
| 008 | [Decoder v2](008-decoder-v2.md)                   | 2026-05-21 | Approved    | Composable decoder text: SOLID-split engine (Rng/GlyphSource/Cell/Ripple/Scheduler) + thin Lit element; both-way "stone-in-water" ripple as single primitive; seedable PRNG; configurable splash; whitespace inert. |
| 009 | [Telemetry hierarchy](009-telemetry-hierarchy.md) | 2026-05-21 | Implemented (Storybook) | ETA in dial `sub` (plain when digits, decoder-jitter on `·····` placeholder); under-block speed+size column with digit-presence rule — units / `/` / rush-placeholders decoded, digits plain; `<stable-num>` primitive anchors volatile slots; canonical 007 stage labels; no filenames. VM-side wiring pending. |
| 010 | [RUN-stage addresses](010-run-addresses.md) | 2026-05-21 | In Progress | RUN-only `<run-addresses>` component fills under-block slot when telemetry hides; all reachable IPs as rows; whole row is copy hit target with HIG feedback; decoder-v2 on identity reveal + slow idle; four-layer copy event (breath + icon swap + address highlight + GSAP micro-bounce). |
| 011 | [Dial frame FLIP](011-dial-frame-flip.md) | 2026-05-21 | Superseded | FLIP wiring produced inverted motion; first pivot (top-anchor cycle) stuck dial to viewport top; final pivot reserves `.frame { min-height: 480px }` covering worst-case stage so centred cycle never re-centres and dial position is constant across all stages. No animation needed. |
| 012 | [Drop decoder v1, unbrand v2](012-decoder-drop-v1.md) | 2026-05-21 | Approved | Delete `decoder-text` (v1) + its lone story; rename `decoder-v2/` → `decoder/`, tag `decoder-v2` → `rune-decoder`; all live callers already on v2. |
