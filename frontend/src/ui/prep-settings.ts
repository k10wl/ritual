/**
 * IDLE-only advanced-settings disclosure. Composes <rune-disclosure> +
 * <rune-field> × 2 (port, memoryMB). Validity computed from per-field
 * injected validators (Formik-style). Emits `change` on every keystroke
 * (for Start enable/disable) and `submit` when the consumer asks the
 * form to submit (read on Start).
 *
 * See design-log/014-prep-advanced-settings.md.
 */

import { LitElement, css, html } from "lit";
import { customElement, property, queryAll } from "lit/decorators.js";
import "./primitives/rune-disclosure";
import "./primitives/rune-field";
import {
    composeValidators,
    type RuneField,
    type RuneFieldValidator,
} from "./primitives/rune-field";

export interface PrepSettings {
    port: number;
    memoryMB: number;
}

export interface PrepSettingsChangeDetail {
    valid: boolean;
    settings: PrepSettings | null;
}

const required: RuneFieldValidator = (v) =>
    v.trim() === "" ? "Required." : null;

const integer: RuneFieldValidator = (v) =>
    /^-?\d+$/.test(v) ? null : "Whole number only.";

const range = (lo: number, hi: number): RuneFieldValidator => (v) => {
    const n = Number(v);
    return n < lo || n > hi ? `Must be between ${lo} and ${hi}.` : null;
};

const multipleOf = (m: number): RuneFieldValidator => (v) =>
    Number(v) % m === 0 ? null : `Must be a multiple of ${m}.`;

const portValidator = composeValidators(required, integer, range(1, 65535));
const memoryValidator = composeValidators(
    required,
    integer,
    range(512, 65536),
    multipleOf(512),
);

@customElement("prep-settings")
export class PrepSettingsEl extends LitElement {
    @property({ type: Object }) config: PrepSettings = { port: 25565, memoryMB: 4096 };

    @queryAll("rune-field") private _fields!: NodeListOf<RuneField>;

    static styles = css`
        :host {
            display: block;
            font-family: var(--font-mono);
        }
        form {
            display: flex;
            flex-direction: column;
            gap: var(--space-4);
        }
    `;

    render() {
        return html`
            <rune-disclosure>
                <span slot="summary">Advanced</span>
                <form @input=${this.#onInput} @submit=${this.#onSubmit}>
                    <rune-field
                        type="number"
                        name="port"
                        label="Port"
                        .value=${String(this.config.port)}
                        .validate=${portValidator}
                        hint="Port must be 1–65535."
                    ></rune-field>
                    <rune-field
                        type="number"
                        name="memoryMB"
                        label="Memory (MB)"
                        .value=${String(this.config.memoryMB)}
                        .validate=${memoryValidator}
                        hint="Memory must be ≥ 512 MB, in 512 MB steps."
                    ></rune-field>
                </form>
            </rune-disclosure>
        `;
    }

    /** Read current settings; returns null if any field is invalid. */
    read(): PrepSettings | null {
        const values: Record<string, string> = {};
        for (const f of this._fields) {
            if (f.invalid) return null;
            values[f.name] = f.value;
        }
        const port = Number(values.port);
        const memoryMB = Number(values.memoryMB);
        if (!Number.isFinite(port) || !Number.isFinite(memoryMB)) return null;
        return { port, memoryMB };
    }

    /** True when every field is currently valid. */
    isValid(): boolean {
        if (!this._fields) return false;
        for (const f of this._fields) {
            if (f.invalid) return false;
        }
        return true;
    }

    #onInput = () => {
        const settings = this.read();
        const detail: PrepSettingsChangeDetail = {
            valid: settings !== null,
            settings,
        };
        this.dispatchEvent(new CustomEvent("change", {
            bubbles: true,
            composed: true,
            detail,
        }));
    };

    #onSubmit = (e: Event) => {
        e.preventDefault();
        const settings = this.read();
        if (!settings) return;
        this.dispatchEvent(new CustomEvent("submit", {
            bubbles: true,
            composed: true,
            detail: settings,
        }));
    };
}

declare global {
    interface HTMLElementTagNameMap {
        "prep-settings": PrepSettingsEl;
    }
}
