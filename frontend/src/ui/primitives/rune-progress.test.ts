import { html, fixture, expect } from "@open-wc/testing";
import "./rune-progress";
import type { RuneProgress } from "./rune-progress";

describe("rune-progress", () => {
    it("renders ring by default", async () => {
        const el = await fixture<RuneProgress>(html`<rune-progress value="0.5"></rune-progress>`);
        expect(el.variant).to.equal("ring");
        const ring = el.shadowRoot!.querySelector(".ring");
        expect(ring).to.exist;
    });

    it("renders linear bar when variant=linear", async () => {
        const el = await fixture<RuneProgress>(html`<rune-progress variant="linear" value="0.3"></rune-progress>`);
        const bar = el.shadowRoot!.querySelector(".bar");
        expect(bar).to.exist;
        expect((bar as HTMLElement).style.transform).to.include("scaleX(0.3)");
    });

    it("marks indeterminate when value omitted", async () => {
        const el = await fixture<RuneProgress>(html`<rune-progress></rune-progress>`);
        const ring = el.shadowRoot!.querySelector(".ring")!;
        expect(ring.classList.contains("indeterminate")).to.equal(true);
    });

    it("clamps value to 0..1", async () => {
        const high = await fixture<RuneProgress>(html`<rune-progress variant="linear" value="2"></rune-progress>`);
        const low  = await fixture<RuneProgress>(html`<rune-progress variant="linear" value="-1"></rune-progress>`);
        expect((high.shadowRoot!.querySelector(".bar")! as HTMLElement).style.transform).to.include("scaleX(1)");
        expect((low.shadowRoot!.querySelector(".bar")! as HTMLElement).style.transform).to.include("scaleX(0)");
    });

    it("sets aria attributes", async () => {
        const el = await fixture<RuneProgress>(html`<rune-progress variant="linear" value="0.7"></rune-progress>`);
        const bar = el.shadowRoot!.querySelector("[role=\"progressbar\"]")!;
        expect(bar.getAttribute("aria-valuemin")).to.equal("0");
        expect(bar.getAttribute("aria-valuemax")).to.equal("1");
        expect(bar.getAttribute("aria-valuenow")).to.equal("0.7");
    });
});
