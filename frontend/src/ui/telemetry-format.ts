const KB = 1024;
const MB = KB * 1024;
const GB = MB * 1024;

interface Unit { div: number; suffix: string; decimals: number; }

function pickUnit(n: number): Unit {
    if (n >= GB) return { div: GB, suffix: "GB", decimals: 2 };
    if (n >= MB) return { div: MB, suffix: "MB", decimals: 1 };
    if (n >= KB) return { div: KB, suffix: "KB", decimals: 1 };
    return { div: 1, suffix: "B", decimals: 0 };
}

function fmt(n: number, decimals: number): string {
    return decimals === 0 ? `${Math.round(n)}` : n.toFixed(decimals);
}

export interface SpeedParts { value: string; unit: string; }
export interface SizeParts { done: string; total: string; unit: string; }

export function formatSpeed(bps: number): SpeedParts {
    if (!Number.isFinite(bps) || bps <= 0) return { value: "0", unit: "B/s" };
    const u = pickUnit(bps);
    return { value: fmt(bps / u.div, u.decimals), unit: `${u.suffix}/s` };
}

export function formatSize(done: number, total: number): SizeParts {
    if (total <= 0) {
        const u = pickUnit(done);
        return { done: fmt(done / u.div, u.decimals), total: "", unit: u.suffix };
    }
    const u = pickUnit(total);
    return {
        done: fmt(done / u.div, u.decimals),
        total: fmt(total / u.div, u.decimals),
        unit: u.suffix,
    };
}

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
