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

import { LitElement, css, html, nothing } from "lit";
import { customElement, property, queryAll, state } from "lit/decorators.js";
import "./primitives/rune-disclosure";
import "./primitives/rune-field";
import "./primitives/rune-button";
import "./primitives/rune-sheet";
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

/** Direction of a server-free sync gesture (design-log/031). */
export type SyncDirection = "download" | "upload";

export interface PrepSettingsSyncDetail {
    direction: SyncDirection;
}

/**
 * Confirm-dialog copy per design-log/031 §Q8. Force-ish gestures: the
 * primary action is destructive-in-spirit, so Cancel is the safe default and
 * the body spells out the consequence.
 */
const SYNC_COPY: Record<SyncDirection, { heading: string; body: string; confirm: string }> = {
    download: {
        heading: "Get latest from remote?",
        body: "Remote worlds overwrite your local copy. Local-only files in the synced folder are removed.",
        confirm: "Download",
    },
    upload: {
        heading: "Publish local to remote?",
        body: "Your local worlds become a new remote ref. This cannot be undone from inside the app.",
        confirm: "Upload",
    },
};

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

    /** Which sync gesture is awaiting confirmation; null when no dialog open. */
    @state() private _confirming: SyncDirection | null = null;

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

        .sync {
            margin-top: var(--space-4);
            padding-top: var(--space-4);
            border-top: 1px solid var(--stone-bevel);
            display: flex;
            flex-direction: column;
            gap: var(--space-2);
        }
        .sync-row {
            display: grid;
            grid-template-columns: 1fr 1fr;
            gap: var(--space-3);
        }
        .sync-hint {
            margin: 0;
            color: var(--text-muted);
            font-size: var(--fs-caption);
        }
        .confirm-body {
            margin: 0;
            color: var(--text-muted);
            line-height: 1.5;
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
                        name="memoryGB"
                        label="Memory (GB)"
                        .value=${formatGB(this.config.memoryMB)}
                        .validate=${memoryValidator}
                        hint="At least ${MIN_MEMORY_GB} GB."
                    ></rune-field>
                </form>
                <div class="sync">
                    <div class="sync-row">
                        <rune-button variant="tinted" size="sm" @press=${this.#askDownload}>
                            Download
                        </rune-button>
                        <rune-button variant="tinted" size="sm" @press=${this.#askUpload}>
                            Upload
                        </rune-button>
                    </div>
                    <p class="sync-hint">Get remote · publish local. No server launch.</p>
                </div>
            </rune-disclosure>
            ${this.#renderConfirm()}
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

    #renderConfirm() {
        if (this._confirming === null) return nothing;
        const copy = SYNC_COPY[this._confirming];
        return html`
            <rune-sheet
                open
                heading=${copy.heading}
                @dismiss=${this.#cancelConfirm}
            >
                <p class="confirm-body">${copy.body}</p>
                <rune-button slot="footer" variant="tinted" @press=${this.#cancelConfirm}>
                    Cancel
                </rune-button>
                <rune-button slot="footer" variant="primary" @press=${this.#confirmSync}>
                    ${copy.confirm}
                </rune-button>
            </rune-sheet>
        `;
    }

    #askDownload = () => { this._confirming = "download"; };
    #askUpload = () => { this._confirming = "upload"; };
    #cancelConfirm = () => { this._confirming = null; };

    #confirmSync = () => {
        const direction = this._confirming;
        this._confirming = null;
        if (direction === null) return;
        const detail: PrepSettingsSyncDetail = { direction };
        this.dispatchEvent(new CustomEvent("sync", {
            bubbles: true,
            composed: true,
            detail,
        }));
    };
}

declare global {
    interface HTMLElementTagNameMap {
        "prep-settings": PrepSettingsEl;
    }
}
