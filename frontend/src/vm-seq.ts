/**
 * True when `incoming` is strictly newer than `current` by Seq
 * (design-log/051 Q11). Wails/WebView2 delivery is fire-and-forget and does
 * not guarantee execution order matches submission order under load, so a
 * stale duplicate can execute after a newer snapshot already applied.
 * Comparing Seq — a strictly increasing counter stamped once per backend
 * emit — makes a late straggler detectable and ignorable, no
 * acknowledgment/retry round-trip needed.
 */
export function isNewerSnapshot(current: { seq: number }, incoming: { seq: number }): boolean {
    return incoming.seq > current.seq;
}
