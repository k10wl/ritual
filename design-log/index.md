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
