import { expect } from "@open-wc/testing";
import {
    mark,
    sample,
    summarize,
    dayKey,
    monthKey,
    isoWeekKey,
    type Backup,
    type RetentionRules,
} from "./retention-model";

const utc = (iso: string): Date => new Date(iso);
const b = (iso: string): Backup => ({ id: iso, date: utc(iso) });
const rules = (r: Partial<RetentionRules>): RetentionRules => ({
    keepLast: 0,
    keepDaily: 0,
    keepWeekly: 0,
    keepMonthly: 0,
    ...r,
});
const keptIds = (m: ReturnType<typeof mark>) => m.filter((x) => x.kept).map((x) => x.id);

describe("retention-model — UTC bucket keys (Go parity)", () => {
    it("dayKey / monthKey are UTC calendar keys", () => {
        expect(dayKey(utc("2026-04-14T23:30:00Z"))).to.equal("2026-04-14");
        expect(monthKey(utc("2026-04-14T23:30:00Z"))).to.equal("2026-04");
    });

    it("isoWeekKey matches Go's ISOWeek (Thursday rule, ISO year rollover)", () => {
        // 2026-01-01 is a Thursday → ISO week 1 of 2026.
        expect(isoWeekKey(utc("2026-01-01T12:00:00Z"))).to.equal("2026-W01");
        // 2025-12-29 (Mon) shares that ISO week — its Thursday is 2026-01-01.
        expect(isoWeekKey(utc("2025-12-29T00:00:00Z"))).to.equal("2026-W01");
    });
});

describe("retention-model — mark() union (Go markKeys parity)", () => {
    it("empty history → nothing marked", () => {
        expect(mark([], rules({ keepLast: 5 }))).to.deep.equal([]);
    });

    it("all-zero rules → every backup pruned", () => {
        const m = mark([b("2026-06-04T10:00:00Z"), b("2026-06-03T10:00:00Z")], rules({}));
        expect(m.every((x) => !x.kept)).to.equal(true);
    });

    it("keepLast:N protects the N newest by position", () => {
        const m = mark(
            [b("2026-06-02T10:00:00Z"), b("2026-06-04T10:00:00Z"), b("2026-06-03T10:00:00Z")],
            rules({ keepLast: 2 }),
        );
        // sorted newest-first: 06-04, 06-03, 06-02 → first two kept.
        expect(keptIds(m)).to.deep.equal(["2026-06-04T10:00:00Z", "2026-06-03T10:00:00Z"]);
        expect(m[0].tiers).to.deep.equal(["last"]);
    });

    it("daily dedups within a UTC day and respects the budget", () => {
        const m = mark(
            [
                b("2026-06-04T20:00:00Z"), // A
                b("2026-06-04T08:00:00Z"), // B (same day as A)
                b("2026-06-03T10:00:00Z"), // C
                b("2026-06-01T10:00:00Z"), // D
            ],
            rules({ keepLast: 1, keepDaily: 2 }),
        );
        // last→A; daily→A(06-04) then C(06-03); B same-day skipped; D over budget.
        expect(keptIds(m)).to.deep.equal(["2026-06-04T20:00:00Z", "2026-06-03T10:00:00Z"]);
        expect(m[0].tiers).to.deep.equal(["last", "daily"], "highest-priority order: last before daily");
        expect(m.find((x) => x.id === "2026-06-03T10:00:00Z")!.tiers).to.deep.equal(["daily"]);
        expect(m.find((x) => x.id === "2026-06-04T08:00:00Z")!.kept).to.equal(false);
        expect(m.find((x) => x.id === "2026-06-01T10:00:00Z")!.kept).to.equal(false);
    });

    it("monthly spreads one survivor per UTC month up to the budget", () => {
        const m = mark(
            [
                b("2026-06-10T10:00:00Z"),
                b("2026-05-10T10:00:00Z"),
                b("2026-04-10T10:00:00Z"),
                b("2026-03-10T10:00:00Z"),
            ],
            rules({ keepMonthly: 2 }),
        );
        // newest two distinct months protected: June, May.
        expect(keptIds(m)).to.deep.equal(["2026-06-10T10:00:00Z", "2026-05-10T10:00:00Z"]);
        expect(m[0].tiers).to.deep.equal(["monthly"]);
    });

    it("union: a backup protected by several tiers carries all, highest-first", () => {
        const m = mark(
            [b("2026-06-04T10:00:00Z"), b("2026-06-03T10:00:00Z")],
            rules({ keepLast: 1, keepDaily: 1, keepWeekly: 1, keepMonthly: 1 }),
        );
        // The newest is the first unseen bucket for every tier → all four.
        expect(m[0].tiers).to.deep.equal(["last", "daily", "weekly", "monthly"]);
        expect(m[0].kept).to.equal(true);
    });
});

describe("retention-model — summarize() + sample()", () => {
    it("summarize counts survivors and the whole-day span", () => {
        const m = mark(
            [b("2026-06-04T10:00:00Z"), b("2026-06-03T10:00:00Z"), b("2026-06-01T10:00:00Z")],
            rules({ keepLast: 1 }),
        );
        const s = summarize(m);
        expect(s.total).to.equal(3);
        expect(s.kept).to.equal(1);
        expect(s.spanDays).to.equal(3, "06-01 → 06-04 is 3 whole days");
        expect(s.oldestSurvivor!.toISOString()).to.equal("2026-06-04T10:00:00.000Z");
    });

    it("sample() is deterministic, newest-first, and spans roughly 95 days", () => {
        const now = new Date("2026-06-04T12:00:00Z");
        const a = sample(now);
        const c = sample(now);
        expect(a.map((x) => x.id)).to.deep.equal(c.map((x) => x.id), "no randomness — same now ⇒ same history");
        expect(a.length).to.be.greaterThan(20);
        for (let i = 1; i < a.length; i++) {
            expect(a[i - 1].date.getTime()).to.be.greaterThanOrEqual(a[i].date.getTime());
        }
        const spanDays = (a[0].date.getTime() - a[a.length - 1].date.getTime()) / (24 * 3600 * 1000);
        expect(spanDays).to.be.greaterThan(80);
    });
});
