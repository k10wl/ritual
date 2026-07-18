import { html } from "lit";
import { fixture, expect, oneEvent } from "@open-wc/testing";
import "./rune-stack";
import type { RuneStack } from "./rune-stack";
import type { NavView } from "../contexts/nav-context";

const view = (id: string, title = id): NavView => ({
    id,
    title,
    render: () => html`<p class="body-${id}">${id} body</p>`,
});

// Force the settle synchronously: real transitionend is timing-flaky, so the
// trim path is driven by dispatching the transform transitionend on the track.
const settle = (el: RuneStack) => {
    const track = el.shadowRoot!.querySelector(".track")!;
    track.dispatchEvent(new TransitionEvent("transitionend", { propertyName: "transform" }));
};

const panes = (el: RuneStack) => el.shadowRoot!.querySelectorAll(".pane");
const trackShift = (el: RuneStack) =>
    (el.shadowRoot!.querySelector(".track") as HTMLElement).style.getPropertyValue("--i");

describe("rune-stack", () => {
    it("starts at the root with depth 0 and only the root pane", async () => {
        const el = await fixture<RuneStack>(html`<rune-stack><span>root</span></rune-stack>`);
        expect(el.depth).to.equal(0);
        expect(panes(el).length).to.equal(1);
        expect(trackShift(el)).to.equal("0");
    });

    it("push mounts a pane, increases depth, and shifts the track", async () => {
        const el = await fixture<RuneStack>(html`<rune-stack><span>root</span></rune-stack>`);
        el.push(view("files"));
        await el.updateComplete;
        expect(el.depth).to.equal(1);
        expect(panes(el).length).to.equal(2);
        expect(trackShift(el)).to.equal("1");
        expect(el.shadowRoot!.querySelector(".body-files")).to.exist;
    });

    it("nests arbitrarily and unwinds one level per pop", async () => {
        const el = await fixture<RuneStack>(html`<rune-stack><span>root</span></rune-stack>`);
        el.push(view("a"));
        await el.updateComplete;
        el.push(view("b"));
        await el.updateComplete;
        el.push(view("c"));
        await el.updateComplete;
        expect(el.depth).to.equal(3);

        el.pop();
        await el.updateComplete;
        expect(el.depth).to.equal(2);
        expect(trackShift(el)).to.equal("2");
    });

    it("keeps the leaving pane mounted until the slide settles, then trims it", async () => {
        const el = await fixture<RuneStack>(html`<rune-stack><span>root</span></rune-stack>`);
        el.push(view("files"));
        await el.updateComplete;
        el.pop();
        await el.updateComplete;
        // Mid-leave: depth is 0 but the pane is still in the DOM, sliding out.
        expect(el.depth).to.equal(0);
        expect(panes(el).length).to.equal(2);
        settle(el);
        await el.updateComplete;
        // Settled: the popped pane is unmounted.
        expect(panes(el).length).to.equal(1);
    });

    it("popToRoot collapses the whole stack", async () => {
        const el = await fixture<RuneStack>(html`<rune-stack><span>root</span></rune-stack>`);
        el.push(view("a"));
        await el.updateComplete;
        el.push(view("b"));
        await el.updateComplete;
        el.popToRoot();
        await el.updateComplete;
        expect(el.depth).to.equal(0);
        expect(trackShift(el)).to.equal("0");
        settle(el);
        await el.updateComplete;
        expect(panes(el).length).to.equal(1);
    });

    it("the ← back bar pops the top view", async () => {
        const el = await fixture<RuneStack>(html`<rune-stack><span>root</span></rune-stack>`);
        el.push(view("files", "Files"));
        await el.updateComplete;
        const back = el.shadowRoot!.querySelector(".back") as HTMLElement;
        expect(back).to.exist;
        back.dispatchEvent(new CustomEvent("press", { bubbles: true, composed: true }));
        await el.updateComplete;
        expect(el.depth).to.equal(0);
    });

    it("Escape pops the top view", async () => {
        const el = await fixture<RuneStack>(html`<rune-stack><span>root</span></rune-stack>`);
        el.push(view("a"));
        await el.updateComplete;
        el.push(view("b"));
        await el.updateComplete;
        window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
        await el.updateComplete;
        expect(el.depth).to.equal(1);
    });

    it("ArrowLeft pops the top view", async () => {
        const el = await fixture<RuneStack>(html`<rune-stack><span>root</span></rune-stack>`);
        el.push(view("a"));
        await el.updateComplete;
        window.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowLeft" }));
        await el.updateComplete;
        expect(el.depth).to.equal(0);
    });

    it("ArrowLeft inside a text field does not pop (caret wins)", async () => {
        const el = await fixture<RuneStack>(html`<rune-stack><span>root</span></rune-stack>`);
        el.push({ id: "form", title: "Form", render: () => html`<input class="f" />` });
        await el.updateComplete;
        const input = el.shadowRoot!.querySelector("input.f") as HTMLInputElement;
        input.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowLeft", bubbles: true, composed: true }));
        await el.updateComplete;
        expect(el.depth).to.equal(1);
    });

    it("blurs the focused element on navigation (no stranded focus)", async () => {
        const el = await fixture<RuneStack>(html`<rune-stack><span>root</span></rune-stack>`);
        el.push({ id: "form", title: "Form", render: () => html`<input class="f" />` });
        await el.updateComplete;
        const input = el.shadowRoot!.querySelector("input.f") as HTMLInputElement;
        input.focus();
        expect(el.shadowRoot!.activeElement).to.equal(input);
        el.pop();
        await el.updateComplete;
        expect(el.shadowRoot!.activeElement).to.not.equal(input);
    });

    it("Escape at the root is a no-op", async () => {
        const el = await fixture<RuneStack>(html`<rune-stack><span>root</span></rune-stack>`);
        window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
        await el.updateComplete;
        expect(el.depth).to.equal(0);
    });

    it("pop at the root is a no-op", async () => {
        const el = await fixture<RuneStack>(html`<rune-stack><span>root</span></rune-stack>`);
        el.pop();
        await el.updateComplete;
        expect(el.depth).to.equal(0);
        expect(panes(el).length).to.equal(1);
    });

    it("emits navigate with the new depth on each move", async () => {
        const el = await fixture<RuneStack>(html`<rune-stack><span>root</span></rune-stack>`);
        setTimeout(() => el.push(view("files")));
        const ev = await oneEvent(el, "navigate");
        expect(ev.detail.depth).to.equal(1);
    });
});
