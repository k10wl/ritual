import { html } from "lit";
import { fixture, expect, oneEvent } from "@open-wc/testing";
import "./retention-rules";
import type { RetentionChangeDetail, RetentionRulesEl } from "./retention-rules";
import type { Backup, RetentionRules } from "./retention-model";

const NOW = new Date("2026-06-04T12:00:00Z");

// Three backups on three distinct days for deterministic mark() assertions.
const HISTORY: Backup[] = [
    { id: "c", date: new Date("2026-06-04T10:00:00Z") },
    { id: "b", date: new Date("2026-06-03T10:00:00Z") },
    { id: "a", date: new Date("2026-06-01T10:00:00Z") },
];

const r = (o: Partial<RetentionRules>): RetentionRules => ({
    keepLast: 0,
    keepDaily: 0,
    keepWeekly: 0,
    keepMonthly: 0,
    ...o,
});

const mount = (local: RetentionRules, opts: Partial<RetentionRulesEl> = {}) =>
    fixture<RetentionRulesEl>(
        html`<retention-rules
            .now=${NOW}
            .local=${local}
            .remote=${(opts.remote as RetentionRules) ?? r({ keepLast: 1 })}
            .localBackups=${HISTORY}
            .remoteBackups=${HISTORY}
        ></retention-rules>`,
    );

const segs = (el: RetentionRulesEl) => [...el.shadowRoot!.querySelectorAll("rune-segmented")] as HTMLElement[];
const text = (el: RetentionRulesEl) => el.shadowRoot!.textContent!.replace(/\s+/g, " ").trim();

// rune-segmented[0] is the scope switch; tiers are [1..4] = last/daily/weekly/monthly.
const tierSeg = (el: RetentionRulesEl, i: number) => segs(el)[1 + i];
const setSeg = (seg: HTMLElement, value: string) =>
    seg.dispatchEvent(new CustomEvent("change", { detail: { value }, bubbles: true, composed: true }));

describe("retention-rules", () => {
    it("renders a scope switch + four tier controls", async () => {
        const el = await mount(r({ keepLast: 2 }));
        expect(segs(el).length).to.equal(5, "1 scope switch + 4 tier segmented controls");
        expect(text(el)).to.contain("Keep last");
        expect(text(el)).to.contain("Keep monthly");
    });

    it("summary reflects the real mark() over the history", async () => {
        const el = await mount(r({ keepLast: 2 }));
        // keepLast:2 over 3 backups → kept 2 of 3.
        expect(text(el)).to.contain("Keeping 2 of 3 backups");
    });

    it("editing a tier emits change {local, remote} with the new value", async () => {
        const el = await mount(r({ keepLast: 2 }));
        setTimeout(() => setSeg(tierSeg(el, 0), "1")); // keep_last → 1
        const ev = await oneEvent(el, "change");
        const d = ev.detail as RetentionChangeDetail;
        expect(d.local.keepLast).to.equal(1);
        expect(d.remote.keepLast).to.equal(1, "the untouched remote side is carried through unchanged");
    });

    it("tightening keep_last updates the live summary", async () => {
        const el = await mount(r({ keepLast: 2 }));
        setSeg(tierSeg(el, 0), "1");
        await el.updateComplete;
        expect(text(el)).to.contain("Keeping 1 of 3 backups");
    });

    it("keep_last:0 shows the caution copy", async () => {
        const el = await mount(r({ keepDaily: 1 }));
        expect(el.shadowRoot!.querySelector(".caution")).to.exist;
        expect(text(el)).to.contain("keep last");
    });

    it("non-zero keep_last hides the caution", async () => {
        const el = await mount(r({ keepLast: 1 }));
        expect(el.shadowRoot!.querySelector(".caution")).to.not.exist;
    });

    it("scope switch edits the remote side independently", async () => {
        const el = await mount(r({ keepLast: 2 }), { remote: r({ keepLast: 3 }) });
        // Switch to remote.
        setSeg(segs(el)[0], "remote");
        await el.updateComplete;
        // The first tier control now reflects the remote rule (3).
        expect((tierSeg(el, 0) as HTMLElement & { value: string }).value).to.equal("3");

        // Edit it → change carries the edited remote, untouched local.
        setTimeout(() => setSeg(tierSeg(el, 0), "4"));
        const ev = await oneEvent(el, "change");
        const d = ev.detail as RetentionChangeDetail;
        expect(d.remote.keepLast).to.equal(4);
        expect(d.local.keepLast).to.equal(2, "switching scope must not mutate the other side");
    });

    it("renders timeline ticks for every backup", async () => {
        const el = await mount(r({ keepLast: 2 }));
        expect(el.shadowRoot!.querySelectorAll(".tick").length).to.equal(HISTORY.length);
    });
});
