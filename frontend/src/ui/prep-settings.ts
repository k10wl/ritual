/**
 * IDLE-only advanced-settings disclosure. Composes <rune-disclosure> +
 * <rune-field> × 2 (port, memory). Validity computed from per-field
 * injected validators (Formik-style). Emits `change` on every keystroke
 * (for Start enable/disable) and `submit` when the consumer asks the
 * form to submit (read on Start).
 *
 * Memory is presented to the user in GB (whole numbers, ≥ 4) but the
 * public contract stays in MB to match the backend (`-Xmx<N>M`).
 * Conversion happens at the edges of this element only.
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

const MIN_MEMORY_GB = 4;
const MAX_MEMORY_GB = 64;

/** Accept "4", "4.5", "4,5" — comma is the decimal separator in many locales. */
const parseGB = (raw: string): number => Number(raw.replace(",", "."));

const memoryShape: RuneFieldValidator = (v) =>
    /^\d+([.,]\d+)?$/.test(v) ? null : "Numbers only (e.g. 4 or 4.5).";

const memoryRange: RuneFieldValidator = (v) => {
    const n = parseGB(v);
    return n < MIN_MEMORY_GB || n > MAX_MEMORY_GB
        ? `Must be between ${MIN_MEMORY_GB} and ${MAX_MEMORY_GB}.`
        : null;
};

const formatGB = (memoryMB: number): string => {
    const gb = Math.max(MIN_MEMORY_GB, memoryMB / 1024);
    return String(+gb.toFixed(1));
};

const portValidator = composeValidators(required, integer, range(1, 65535));
const memoryValidator = composeValidators(required, memoryShape, memoryRange);

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
            display: grid;
            grid-template-columns: 1fr 1fr;
            gap: var(--space-3) var(--space-4);
        }
        form > rune-field { min-width: 0; }
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
                        name="memoryGB"
                        label="Memory (GB)"
                        .value=${formatGB(this.config.memoryMB)}
                        .validate=${memoryValidator}
                        hint="At least ${MIN_MEMORY_GB} GB."
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
        const memoryGB = parseGB(values.memoryGB);
        if (!Number.isFinite(port) || !Number.isFinite(memoryGB)) return null;
        return { port, memoryMB: Math.round(memoryGB * 1024) };
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
