/**
 * IDLE-only advanced-settings disclosure. Composes <rune-disclosure> +
 * <rune-field> × 2 (port, memoryMB). Form-driven validity per 014 §HIG
 * hint rule; emits `change` on every keystroke (for Start enable/disable)
 * and `submit` when the consumer asks the form to submit (read on Start).
 *
 * See design-log/014-prep-advanced-settings.md.
 */

import { LitElement, css, html } from "lit";
import { customElement, property, query } from "lit/decorators.js";
import "./primitives/rune-disclosure";
import "./primitives/rune-field";

export interface PrepSettings {
    port: number;
    memoryMB: number;
}

export interface PrepSettingsChangeDetail {
    valid: boolean;
    settings: PrepSettings | null;
}

@customElement("prep-settings")
export class PrepSettingsEl extends LitElement {
    @property({ type: Object }) config: PrepSettings = { port: 25565, memoryMB: 4096 };

    @query("form") private _form!: HTMLFormElement;

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
                        min="1"
                        max="65535"
                        step="1"
                        hint="Port must be 1–65535."
                    ></rune-field>
                    <rune-field
                        type="number"
                        name="memoryMB"
                        label="Memory (MB)"
                        .value=${String(this.config.memoryMB)}
                        min="512"
                        step="512"
                        hint="Memory must be ≥ 512 MB, in 512 MB steps."
                    ></rune-field>
                </form>
            </rune-disclosure>
        `;
    }

    /** Read current settings; returns null if invalid. */
    read(): PrepSettings | null {
        const data = new FormData(this._form);
        const port = Number(data.get("port"));
        const memoryMB = Number(data.get("memoryMB"));
        if (!this._form.checkValidity()) return null;
        if (!Number.isFinite(port) || !Number.isFinite(memoryMB)) return null;
        return { port, memoryMB };
    }

    /** True when the current form values are valid. */
    isValid(): boolean {
        return this._form?.checkValidity() ?? false;
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
