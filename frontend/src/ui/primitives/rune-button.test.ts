import { html, fixture, expect, oneEvent } from "@open-wc/testing";
import "./rune-button";
import type { RuneButton, RuneButtonPressDetail } from "./rune-button";

describe("rune-button", () => {
    it("renders with default variant + size", async () => {
        const el = await fixture<RuneButton>(html`<rune-button>Go</rune-button>`);
        expect(el.variant).to.equal("tinted");
        expect(el.size).to.equal("md");
        expect(el.shadowRoot!.querySelector("button")).to.exist;
    });

    it("reflects variant attribute", async () => {
        const el = await fixture<RuneButton>(html`<rune-button variant="primary">Go</rune-button>`);
        expect(el.getAttribute("variant")).to.equal("primary");
    });

    it("emits `press` with pointer origin on click", async () => {
        const el = await fixture<RuneButton>(html`<rune-button>Go</rune-button>`);
        const btn = el.shadowRoot!.querySelector("button")!;
        setTimeout(() => btn.click(), 0);
        const ev = (await oneEvent(el, "press")) as CustomEvent<RuneButtonPressDetail>;
        expect(ev.detail.origin).to.equal("pointer");
    });

    it("does not emit `press` when disabled", async () => {
        const el = await fixture<RuneButton>(html`<rune-button disabled>Go</rune-button>`);
        const btn = el.shadowRoot!.querySelector("button")!;
        let fired = false;
        el.addEventListener("press", () => { fired = true; });
        btn.click();
        await new Promise((r) => setTimeout(r, 0));
        expect(fired).to.equal(false);
    });

    it("does not emit `press` when loading", async () => {
        const el = await fixture<RuneButton>(html`<rune-button loading>Go</rune-button>`);
        const btn = el.shadowRoot!.querySelector("button")!;
        let fired = false;
        el.addEventListener("press", () => { fired = true; });
        btn.click();
        await new Promise((r) => setTimeout(r, 0));
        expect(fired).to.equal(false);
    });

    it("emits `press` with keyboard origin on Enter", async () => {
        const el = await fixture<RuneButton>(html`<rune-button>Go</rune-button>`);
        const btn = el.shadowRoot!.querySelector("button")!;
        setTimeout(() => btn.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true })), 0);
        const ev = (await oneEvent(el, "press")) as CustomEvent<RuneButtonPressDetail>;
        expect(ev.detail.origin).to.equal("keyboard");
    });

    it("fills slots", async () => {
        const el = await fixture<RuneButton>(html`
            <rune-button>
                <span slot="leading">L</span>
                Body
                <span slot="trailing">T</span>
            </rune-button>
        `);
        const slots = el.shadowRoot!.querySelectorAll("slot");
        expect(slots.length).to.equal(3);
        expect(slots[0].getAttribute("name")).to.equal("leading");
        expect(slots[1].getAttribute("name")).to.equal(null);
        expect(slots[2].getAttribute("name")).to.equal("trailing");
    });
});
