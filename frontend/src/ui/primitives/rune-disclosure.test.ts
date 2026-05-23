import { html, fixture, expect, oneEvent } from "@open-wc/testing";
import "./rune-disclosure";
import type { RuneDisclosure } from "./rune-disclosure";

describe("rune-disclosure", () => {
    it("renders closed by default", async () => {
        const el = await fixture<RuneDisclosure>(html`
            <rune-disclosure>
                <span slot="summary">More</span>
                Body
            </rune-disclosure>
        `);
        expect(el.open).to.equal(false);
        const details = el.shadowRoot!.querySelector("details")!;
        expect(details.open).to.equal(false);
    });

    it("opens when `open` attribute set", async () => {
        const el = await fixture<RuneDisclosure>(html`
            <rune-disclosure open>
                <span slot="summary">More</span>
                Body
            </rune-disclosure>
        `);
        expect(el.open).to.equal(true);
        const details = el.shadowRoot!.querySelector("details")!;
        expect(details.open).to.equal(true);
    });

    it("emits `open` when toggled open", async () => {
        const el = await fixture<RuneDisclosure>(html`
            <rune-disclosure>
                <span slot="summary">More</span>
                Body
            </rune-disclosure>
        `);
        const details = el.shadowRoot!.querySelector("details")!;
        setTimeout(() => { details.open = true; details.dispatchEvent(new Event("toggle")); }, 0);
        const ev = await oneEvent(el, "open");
        expect(ev.type).to.equal("open");
        expect(el.open).to.equal(true);
    });

    it("emits `close` when toggled closed", async () => {
        const el = await fixture<RuneDisclosure>(html`
            <rune-disclosure open>
                <span slot="summary">More</span>
                Body
            </rune-disclosure>
        `);
        const details = el.shadowRoot!.querySelector("details")!;
        setTimeout(() => { details.open = false; details.dispatchEvent(new Event("toggle")); }, 0);
        const ev = await oneEvent(el, "close");
        expect(ev.type).to.equal("close");
        expect(el.open).to.equal(false);
    });
});
