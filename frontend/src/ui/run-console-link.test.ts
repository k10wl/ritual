import { html, fixture, expect, oneEvent } from "@open-wc/testing";
import "./run-console-link";
import type { RunConsoleLink } from "./run-console-link";

describe("run-console-link", () => {
    it("renders a single button row with an accessible label", async () => {
        const el = await fixture<RunConsoleLink>(html`<run-console-link></run-console-link>`);
        const row = el.shadowRoot!.querySelector(".row")!;
        expect(row).to.exist;
        expect(row.getAttribute("role")).to.equal("button");
        expect(row.getAttribute("aria-label")).to.equal("Open server console");
    });

    it("emits `press` on click", async () => {
        const el = await fixture<RunConsoleLink>(html`<run-console-link></run-console-link>`);
        const row = el.shadowRoot!.querySelector(".row") as HTMLElement;
        setTimeout(() => row.click(), 0);
        const ev = await oneEvent(el, "press");
        expect(ev).to.exist;
    });

    it("emits `press` on Enter and Space", async () => {
        for (const key of ["Enter", " "]) {
            const el = await fixture<RunConsoleLink>(html`<run-console-link></run-console-link>`);
            const row = el.shadowRoot!.querySelector(".row") as HTMLElement;
            setTimeout(() => row.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true })), 0);
            const ev = await oneEvent(el, "press");
            expect(ev, `key=${key}`).to.exist;
        }
    });

    it("ignores other keys", async () => {
        const el = await fixture<RunConsoleLink>(html`<run-console-link></run-console-link>`);
        const row = el.shadowRoot!.querySelector(".row") as HTMLElement;
        let fired = false;
        el.addEventListener("press", () => { fired = true; });
        row.dispatchEvent(new KeyboardEvent("keydown", { key: "a", bubbles: true }));
        expect(fired).to.be.false;
    });
});
