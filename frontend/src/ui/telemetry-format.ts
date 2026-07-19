// Size/speed unit formatting moved to Go (internal/gui/projection/format.go,
// design-log/050) — ViewModel.sizeDoneText/sizeTotalText/sizeUnit/speedText/
// speedUnit arrive pre-formatted. ETA duration formatting stays here: it's
// UI copy, not data shaping (design-log/050 decision — the backend drives
// *when* EtaSeconds/uptimeSeconds change, the frontend decides *how* to
// render them).

export const ETA_PLACEHOLDER = "·····";

export function formatEta(seconds: number | null): string {
    if (seconds === null || !Number.isFinite(seconds) || seconds < 0) return ETA_PLACEHOLDER;
    const s = Math.round(seconds);
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    const r = s % 60;
    const mm = m.toString().padStart(2, "0");
    const ss = r.toString().padStart(2, "0");
    if (h > 0) return `${h.toString().padStart(2, "0")}:${mm}:${ss}`;
    return `${mm}:${ss}`;
}
