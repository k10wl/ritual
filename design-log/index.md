# Design Log Index

Catalogue of design logs in order. New entries append to the bottom.

**Status values:** `Draft` · `Approved` · `In Progress` · `Implemented` · `Superseded`

| #   | Title                                             | Date       | Status      | Description                                                                          |
|-----|---------------------------------------------------|------------|-------------|--------------------------------------------------------------------------------------|
| 001 | [Progress Projection](001-progress-projection.md) | 2026-05-17 | Implemented | Two counter layers (logical / wire), three speed flavours, projection as pure picker. |
| 002 | [GUI Reset](002-gui-reset.md)                     | 2026-05-17 | Draft       | Browser CSS reset + tokens + bundled Inter + shared primitives; kill cross-platform drift. |
| 003 | [Encoder Pool](003-encoder-pool.md)               | 2026-05-18 | Implemented | Replace single zstd encoder + mutex with sync.Pool mirror of decoder side; unblocks pull fan-out. |
| 004 | [R2 Retry Decorator](004-r2-retry-decorator.md)   | 2026-05-18 | Implemented | RetryingStorage decorator + RangeGetter capability for mid-stream resume; fix GetStream error attribution. |
