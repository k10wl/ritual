/**
 * Pure retention model — a faithful TS port of the Go `markKeys` union walk
 * (internal/core/retention/retention_mark.go) plus UTC bucket keys, a
 * deterministic sample history, and a summariser. No IO, no DOM — unit-testable
 * on its own. Single source of *truth* stays the Go engine + spec; this mirror
 * lets the UI show what a rule set keeps vs prunes live, without a round-trip per
 * keystroke (design-log/033 §Q3, /039 §Q5).
 *
 * Parity contract with Go: sort newest→oldest; a tier protects a backup iff its
 * running count < ruleN **and** (for daily/weekly/monthly) the UTC bucket is
 * unseen; `kept = tiers.length > 0`. Same union semantics, same budget-per-tier,
 * same UTC boundaries (Date.UTC / getUTC*, no custom calendar).
 */

export type Tier = "last" | "daily" | "weekly" | "monthly";

export interface RetentionRules {
    keepLast: number;
    keepDaily: number;
    keepWeekly: number;
    keepMonthly: number;
}

export interface Backup {
    id: string;
    date: Date;
}

export interface Marked extends Backup {
    tiers: Tier[];
    kept: boolean;
}

// Highest-priority protecting tier first — the timeline colours a survivor by
// its strongest reason (design-log/033 §Q5): last > daily > weekly > monthly.
export const TIER_PRIORITY: Tier[] = ["last", "daily", "weekly", "monthly"];

/** UTC calendar-day key, "YYYY-MM-DD". */
export function dayKey(d: Date): string {
    return `${d.getUTCFullYear()}-${pad2(d.getUTCMonth() + 1)}-${pad2(d.getUTCDate())}`;
}

/** UTC calendar-month key, "YYYY-MM". */
export function monthKey(d: Date): string {
    return `${d.getUTCFullYear()}-${pad2(d.getUTCMonth() + 1)}`;
}

/** ISO-8601 week key, "YYYY-Www" — mirrors Go's time.ISOWeek (Mon-based,
 * Thursday rule, ISO year). */
export function isoWeekKey(d: Date): string {
    const date = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate()));
    const dayNum = (date.getUTCDay() + 6) % 7; // Mon=0 … Sun=6
    date.setUTCDate(date.getUTCDate() - dayNum + 3); // nearest Thursday
    const isoYear = date.getUTCFullYear();
    const firstThursday = new Date(Date.UTC(isoYear, 0, 4));
    const ftDayNum = (firstThursday.getUTCDay() + 6) % 7;
    firstThursday.setUTCDate(firstThursday.getUTCDate() - ftDayNum + 3);
    const week = 1 + Math.round((date.getTime() - firstThursday.getTime()) / (7 * 24 * 3600 * 1000));
    return `${isoYear}-W${pad2(week)}`;
}

/**
 * mark classifies each backup with the tiers that protect it (union). Mirrors Go
 * `markKeys`: newest-first, budget-per-tier, bucket-dedup for daily/weekly/
 * monthly. Returns the input newest-first with `tiers` + `kept`.
 */
export function mark(backups: Backup[], rules: RetentionRules): Marked[] {
    const sorted = [...backups].sort((a, b) => {
        const t = b.date.getTime() - a.date.getTime();
        return t !== 0 ? t : a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
    });

    const tiersByIndex: Tier[][] = sorted.map(() => []);

    // keep_last — the N newest by position.
    for (let i = 0; i < sorted.length && i < rules.keepLast; i++) {
        tiersByIndex[i].push("last");
    }

    bucketTier(sorted, tiersByIndex, rules.keepDaily, dayKey, "daily");
    bucketTier(sorted, tiersByIndex, rules.keepWeekly, isoWeekKey, "weekly");
    bucketTier(sorted, tiersByIndex, rules.keepMonthly, monthKey, "monthly");

    return sorted.map((b, i) => ({
        ...b,
        tiers: TIER_PRIORITY.filter((t) => tiersByIndex[i].includes(t)),
        kept: tiersByIndex[i].length > 0,
    }));
}

function bucketTier(
    sorted: Backup[],
    tiersByIndex: Tier[][],
    budget: number,
    key: (d: Date) => string,
    tier: Tier,
): void {
    const seen = new Set<string>();
    let count = 0;
    for (let i = 0; i < sorted.length; i++) {
        const k = key(sorted[i].date);
        if (!seen.has(k) && count < budget) {
            tiersByIndex[i].push(tier);
            count++;
        }
        seen.add(k);
    }
}

export interface Summary {
    kept: number;
    total: number;
    oldestSurvivor: Date | null;
    spanDays: number;
}

/** summarize counts survivors and the history span (whole UTC days). */
export function summarize(marked: Marked[]): Summary {
    const survivors = marked.filter((m) => m.kept);
    const oldestSurvivor = survivors.length ? survivors[survivors.length - 1].date : null;
    let spanDays = 0;
    if (marked.length >= 2) {
        const newest = marked[0].date.getTime();
        const oldest = marked[marked.length - 1].date.getTime();
        spanDays = Math.round((newest - oldest) / (24 * 3600 * 1000));
    }
    return { kept: survivors.length, total: marked.length, oldestSurvivor, spanDays };
}

/**
 * sample builds a deterministic synthetic history (~31 backups over ~95 days)
 * relative to `now`: monthly-ish spread early, then denser toward the present,
 * with intra-day multiples on the most recent days to exercise keep_last. No
 * randomness (design-log/033 §Q3 — deterministic for stories/tests).
 */
export function sample(now: Date): Backup[] {
    const base = now.getTime();
    const DAY = 24 * 3600 * 1000;
    const HOUR = 3600 * 1000;
    const offsets: number[] = [];

    // Older: roughly one every 6–9 days from ~95 to ~16 days ago.
    for (let d = 95; d > 14; d -= 7) offsets.push(d * DAY);
    // Recent week: one per day.
    for (let d = 14; d >= 1; d -= 1) offsets.push(d * DAY);
    // Today + yesterday: intra-day multiples (exercise keep_last overlap).
    offsets.push(6 * HOUR, 3 * HOUR, 1 * HOUR, DAY + 8 * HOUR, DAY + 2 * HOUR);

    return offsets
        .map((off, i) => {
            const date = new Date(base - off);
            return { id: `${date.toISOString()}#${i}`, date };
        })
        .sort((a, b) => b.date.getTime() - a.date.getTime());
}

function pad2(n: number): string {
    return String(n).padStart(2, "0");
}
