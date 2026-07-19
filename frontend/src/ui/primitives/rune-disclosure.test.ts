import { html, fixture, expect, oneEvent } from "@open-wc/testing";
import "./rune-disclosure";
import type { RuneDisclosure } from "./rune-disclosure";

const summary = (el: RuneDisclosure) =>
    el.shadowRoot!.querySelector<HTMLButtonElement>(".summary")!;

describe("rune-disclosure", () => {
    it("renders closed by default", async () => {
        const el = await fixture<RuneDisclosure>(html`
            <rune-disclosure>
                <span slot="summary">More</span>
                Body
            </rune-disclosure>
        `);
        expect(el.open).to.equal(false);
        expect(summary(el).getAttribute("aria-expanded")).to.equal("false");
    });

    it("reflects `open` attribute to aria-expanded", async () => {
        const el = await fixture<RuneDisclosure>(html`
            <rune-disclosure open>
                <span slot="summary">More</span>
                Body
            </rune-disclosure>
        `);
        expect(el.open).to.equal(true);
        expect(summary(el).getAttribute("aria-expanded")).to.equal("true");
    });

    it("emits `open` when the summary is clicked while closed", async () => {
        const el = await fixture<RuneDisclosure>(html`
            <rune-disclosure>
                <span slot="summary">More</span>
                Body
            </rune-disclosure>
        `);
        setTimeout(() => summary(el).click(), 0);
        const ev = await oneEvent(el, "open");
        expect(ev.type).to.equal("open");
        expect(el.open).to.equal(true);
    });

    it("emits `close` when the summary is clicked while open", async () => {
        const el = await fixture<RuneDisclosure>(html`
            <rune-disclosure open>
                <span slot="summary">More</span>
                Body
            </rune-disclosure>
        `);
        setTimeout(() => summary(el).click(), 0);
        const ev = await oneEvent(el, "close");
        expect(ev.type).to.equal("close");
        expect(el.open).to.equal(false);
    });
});
