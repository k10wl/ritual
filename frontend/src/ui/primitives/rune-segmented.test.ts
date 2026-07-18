import { html } from "lit";
import { fixture, expect, oneEvent } from "@open-wc/testing";
import "./rune-segmented";
import type { RuneSegmented, RuneSegmentedChangeDetail, SegmentOption } from "./rune-segmented";

const OPTS: SegmentOption[] = ["0", "1", "2", "3", "4", "5"].map((v) => ({ value: v, label: v }));

const mount = (value = "2") =>
    fixture<RuneSegmented>(html`<rune-segmented .options=${OPTS} value=${value} label="Keep last"></rune-segmented>`);

const segs = (el: RuneSegmented) => [...el.shadowRoot!.querySelectorAll("[role='radio']")] as HTMLElement[];

describe("rune-segmented", () => {
    it("renders one radio per option inside a labelled radiogroup", async () => {
        const el = await mount();
        expect(el.shadowRoot!.querySelector("[role='radiogroup']")?.getAttribute("aria-label")).to.equal("Keep last");
        expect(segs(el).length).to.equal(6);
    });

    it("marks only the selected segment checked, and only it is a tab stop", async () => {
        const el = await mount("2");
        const s = segs(el);
        expect(s[2].getAttribute("aria-checked")).to.equal("true");
        expect(s[2].getAttribute("tabindex")).to.equal("0");
        expect(s[0].getAttribute("aria-checked")).to.equal("false");
        expect(s[0].getAttribute("tabindex")).to.equal("-1");
    });

    it("click selects and emits change {value}", async () => {
        const el = await mount("2");
        setTimeout(() => segs(el)[4].click());
        const ev = await oneEvent(el, "change");
        expect((ev.detail as RuneSegmentedChangeDetail).value).to.equal("4");
        expect(el.value).to.equal("4");
    });

    it("re-selecting the current value does not emit", async () => {
        const el = await mount("2");
        let fired = false;
        el.addEventListener("change", () => (fired = true));
        segs(el)[2].click();
        await el.updateComplete;
        expect(fired).to.equal(false);
    });

    it("ArrowRight moves selection forward and emits", async () => {
        const el = await mount("2");
        setTimeout(() => segs(el)[2].dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowRight", bubbles: true })));
        const ev = await oneEvent(el, "change");
        expect((ev.detail as RuneSegmentedChangeDetail).value).to.equal("3");
    });

    it("ArrowLeft wraps from the first segment to the last", async () => {
        const el = await mount("0");
        setTimeout(() => segs(el)[0].dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowLeft", bubbles: true })));
        const ev = await oneEvent(el, "change");
        expect((ev.detail as RuneSegmentedChangeDetail).value).to.equal("5");
    });

    it("Home/End jump to the ends", async () => {
        const el = await mount("3");
        setTimeout(() => segs(el)[3].dispatchEvent(new KeyboardEvent("keydown", { key: "Home", bubbles: true })));
        let ev = await oneEvent(el, "change");
        expect((ev.detail as RuneSegmentedChangeDetail).value).to.equal("0");

        setTimeout(() => segs(el)[0].dispatchEvent(new KeyboardEvent("keydown", { key: "End", bubbles: true })));
        ev = await oneEvent(el, "change");
        expect((ev.detail as RuneSegmentedChangeDetail).value).to.equal("5");
    });
});
