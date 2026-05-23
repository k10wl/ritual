import { html, fixture, expect, oneEvent } from "@open-wc/testing";
import "./rune-row";
import type { RuneRow } from "./rune-row";
import type { RuneButtonPressDetail } from "./rune-button";

describe("rune-row", () => {
    it("renders with default flags", async () => {
        const el = await fixture<RuneRow>(html`<rune-row>Body</rune-row>`);
        expect(el.pressable).to.equal(false);
        expect(el.active).to.equal(false);
        expect(el.disabled).to.equal(false);
    });

    it("renders with role=row when not pressable", async () => {
        const el = await fixture<RuneRow>(html`<rune-row>Body</rune-row>`);
        const container = el.shadowRoot!.querySelector(".container")!;
        expect(container.getAttribute("role")).to.equal("row");
        expect(container.getAttribute("tabindex")).to.equal("-1");
    });

    it("renders with role=button + tabindex 0 when pressable", async () => {
        const el = await fixture<RuneRow>(html`<rune-row pressable>Body</rune-row>`);
        const container = el.shadowRoot!.querySelector(".container")!;
        expect(container.getAttribute("role")).to.equal("button");
        expect(container.getAttribute("tabindex")).to.equal("0");
    });

    it("emits `press` on click when pressable", async () => {
        const el = await fixture<RuneRow>(html`<rune-row pressable>Body</rune-row>`);
        const container = el.shadowRoot!.querySelector(".container")! as HTMLElement;
        setTimeout(() => container.click(), 0);
        const ev = (await oneEvent(el, "press")) as CustomEvent<RuneButtonPressDetail>;
        expect(ev.detail.origin).to.equal("pointer");
    });

    it("emits `press` on Enter when pressable", async () => {
        const el = await fixture<RuneRow>(html`<rune-row pressable>Body</rune-row>`);
        const container = el.shadowRoot!.querySelector(".container")! as HTMLElement;
        setTimeout(
            () => container.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true })),
            0,
        );
        const ev = (await oneEvent(el, "press")) as CustomEvent<RuneButtonPressDetail>;
        expect(ev.detail.origin).to.equal("keyboard");
    });

    it("does not emit `press` when disabled", async () => {
        const el = await fixture<RuneRow>(html`<rune-row pressable disabled>Body</rune-row>`);
        const container = el.shadowRoot!.querySelector(".container")! as HTMLElement;
        let fired = false;
        el.addEventListener("press", () => { fired = true; });
        container.click();
        await new Promise((r) => setTimeout(r, 0));
        expect(fired).to.equal(false);
    });

    it("does not emit `press` when not pressable", async () => {
        const el = await fixture<RuneRow>(html`<rune-row>Body</rune-row>`);
        const container = el.shadowRoot!.querySelector(".container")! as HTMLElement;
        let fired = false;
        el.addEventListener("press", () => { fired = true; });
        container.click();
        await new Promise((r) => setTimeout(r, 0));
        expect(fired).to.equal(false);
    });
});
