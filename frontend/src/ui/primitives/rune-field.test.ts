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

    it("reflects type to native input", async () => {
        const el = await fixture<RuneField>(html`<rune-field type="number"></rune-field>`);
        const input = el.shadowRoot!.querySelector("input")! as HTMLInputElement;
        expect(input.type).to.equal("number");
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
        expect(hint.textContent!.trim()).to.equal("1024-65535");
    });

    it("is form-associated", () => {
        const RuneFieldCtor = customElements.get("rune-field") as typeof RuneField;
        expect((RuneFieldCtor as unknown as { formAssociated: boolean }).formAssociated).to.equal(true);
    });
});
