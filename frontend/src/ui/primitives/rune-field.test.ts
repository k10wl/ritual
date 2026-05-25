import { html, fixture, expect, oneEvent } from "@open-wc/testing";
import "./rune-field";
import type { RuneField, RuneFieldChangeDetail } from "./rune-field";

describe("rune-field", () => {
    it("renders with label + input", async () => {
        const el = await fixture<RuneField>(html`<rune-field label="Port" value="25565"></rune-field>`);
        expect(el.shadowRoot!.querySelector("label")!.textContent).to.equal("Port");
        const input = el.shadowRoot!.querySelector("input")! as HTMLInputElement;
        expect(input.value).to.equal("25565");
    });

    it("forwards type=number as inputmode=decimal on a text input", async () => {
        // Native <input type="number"> silently masks invalid input as "" in
        // Chrome/Safari and blocks valid keystrokes inconsistently; rune-field
        // always renders type="text" and signals intent via inputmode. See
        // rune-field.ts file header §"Why `type=\"number\"` is a *prop*…".
        const el = await fixture<RuneField>(html`<rune-field type="number"></rune-field>`);
        const input = el.shadowRoot!.querySelector("input")! as HTMLInputElement;
        expect(input.type).to.equal("text");
        expect(input.getAttribute("inputmode")).to.equal("decimal");
    });

    it("forwards type=text as inputmode=text", async () => {
        const el = await fixture<RuneField>(html`<rune-field type="text"></rune-field>`);
        const input = el.shadowRoot!.querySelector("input")! as HTMLInputElement;
        expect(input.type).to.equal("text");
        expect(input.getAttribute("inputmode")).to.equal("text");
    });

    it("emits `change` with value detail", async () => {
        const el = await fixture<RuneField>(html`<rune-field value="abc"></rune-field>`);
        const input = el.shadowRoot!.querySelector("input")! as HTMLInputElement;
        setTimeout(() => {
            input.value = "xyz";
            input.dispatchEvent(new Event("input"));
            input.dispatchEvent(new Event("change"));
        }, 0);
        const ev = (await oneEvent(el, "change")) as CustomEvent<RuneFieldChangeDetail>;
        expect(ev.detail.value).to.equal("xyz");
        expect(el.value).to.equal("xyz");
    });

    it("respects disabled", async () => {
        const el = await fixture<RuneField>(html`<rune-field disabled></rune-field>`);
        const input = el.shadowRoot!.querySelector("input")! as HTMLInputElement;
        expect(input.disabled).to.equal(true);
    });

    it("renders hint from attribute", async () => {
        const el = await fixture<RuneField>(html`<rune-field hint="1024-65535"></rune-field>`);
        const hint = el.shadowRoot!.querySelector(".hint")!;
        expect(hint.hasAttribute("hidden")).to.equal(false);
        expect(hint.textContent!.trim()).to.equal("1024-65535");
    });

    it("hides hint wrapper when neither attribute nor slot supply content", async () => {
        const el = await fixture<RuneField>(html`<rune-field></rune-field>`);
        const hint = el.shadowRoot!.querySelector(".hint")!;
        expect(hint).to.exist;
        expect(hint.hasAttribute("hidden")).to.equal(true);
    });

    it("reveals hint wrapper when hint-slotted content is appended after mount", async () => {
        const el = await fixture<RuneField>(html`<rune-field></rune-field>`);
        await el.updateComplete;
        const hint = el.shadowRoot!.querySelector(".hint")!;
        expect(hint.hasAttribute("hidden")).to.equal(true);
        const tip = document.createElement("span");
        tip.setAttribute("slot", "hint");
        tip.textContent = "Custom hint";
        el.appendChild(tip);
        // slotchange is dispatched as a microtask after the slot's assigned
        // nodes mutate; yield once so the handler can flip `_hasHintSlot` and
        // trigger the next update cycle before we read the wrapper.
        await new Promise((r) => setTimeout(r, 0));
        await el.updateComplete;
        expect(hint.hasAttribute("hidden")).to.equal(false);
    });

    it("is form-associated", () => {
        const RuneFieldCtor = customElements.get("rune-field") as typeof RuneField;
        expect((RuneFieldCtor as unknown as { formAssociated: boolean }).formAssociated).to.equal(true);
    });
});
