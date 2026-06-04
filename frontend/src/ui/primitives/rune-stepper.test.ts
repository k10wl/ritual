import { html } from "lit";
import { fixture, expect, oneEvent } from "@open-wc/testing";
import "./rune-stepper";
import type { RuneStepper, RuneStepperChangeDetail } from "./rune-stepper";

const mount = (value = 2) =>
    fixture<RuneStepper>(html`<rune-stepper value=${value} label="Keep last"></rune-stepper>`);

const group = (el: RuneStepper) => el.shadowRoot!.querySelector("[role='spinbutton']") as HTMLElement;
const btn = (el: RuneStepper, label: string) =>
    [...el.shadowRoot!.querySelectorAll("button")].find((b) => b.getAttribute("aria-label") === label)!;
const valueText = (el: RuneStepper) => el.shadowRoot!.querySelector(".value")!.textContent!.trim();

describe("rune-stepper", () => {
    it("renders the value inside a spinbutton with aria range", async () => {
        const el = await mount(2);
        const g = group(el);
        expect(valueText(el)).to.equal("2");
        expect(g.getAttribute("aria-valuenow")).to.equal("2");
        expect(g.getAttribute("aria-valuemin")).to.equal("0");
        expect(g.getAttribute("aria-valuemax")).to.equal("5");
        expect(g.getAttribute("aria-label")).to.equal("Keep last");
    });

    it("+ increments and emits change {value}", async () => {
        const el = await mount(2);
        setTimeout(() => btn(el, "Increase").click());
        const ev = await oneEvent(el, "change");
        expect((ev.detail as RuneStepperChangeDetail).value).to.equal(3);
        expect(el.value).to.equal(3);
    });

    it("− decrements and emits", async () => {
        const el = await mount(2);
        setTimeout(() => btn(el, "Decrease").click());
        const ev = await oneEvent(el, "change");
        expect((ev.detail as RuneStepperChangeDetail).value).to.equal(1);
    });

    it("clamps at min (− disabled at 0, no emit)", async () => {
        const el = await mount(0);
        expect(btn(el, "Decrease").disabled).to.equal(true);
        let fired = false;
        el.addEventListener("change", () => (fired = true));
        btn(el, "Decrease").click();
        await el.updateComplete;
        expect(fired).to.equal(false);
        expect(el.value).to.equal(0);
    });

    it("clamps at max (+ disabled at 5, no emit)", async () => {
        const el = await mount(5);
        expect(btn(el, "Increase").disabled).to.equal(true);
        let fired = false;
        el.addEventListener("change", () => (fired = true));
        btn(el, "Increase").click();
        await el.updateComplete;
        expect(fired).to.equal(false);
        expect(el.value).to.equal(5);
    });

    it("keyboard: ArrowUp increments, ArrowDown decrements, Home/End jump", async () => {
        const el = await mount(2);
        const press = (key: string) => group(el).dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true }));

        setTimeout(() => press("ArrowUp"));
        expect((await oneEvent(el, "change")).detail.value).to.equal(3);
        setTimeout(() => press("ArrowDown"));
        expect((await oneEvent(el, "change")).detail.value).to.equal(2);
        setTimeout(() => press("End"));
        expect((await oneEvent(el, "change")).detail.value).to.equal(5);
        setTimeout(() => press("Home"));
        expect((await oneEvent(el, "change")).detail.value).to.equal(0);
    });
});
