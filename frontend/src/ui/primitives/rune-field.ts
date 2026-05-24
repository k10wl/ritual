/**
 * Labelled input primitive — HIG label-above text-field pattern. Form-associated
 * custom element so values participate in <form> submission.
 *
 * HIG: https://developer.apple.com/design/human-interface-guidelines/text-fields
 */

import { LitElement, css, html } from "lit";
import { customElement, property, query, state } from "lit/decorators.js";
import { sharedStyles } from "./_base";

export type RuneFieldType = "text" | "number";

export type RuneFieldValidator = (value: string) => string | null;

export const composeValidators =
    (...rules: RuneFieldValidator[]): RuneFieldValidator =>
    (value) => {
        for (const rule of rules) {
            const err = rule(value);
            if (err !== null) return err;
        }
        return null;
    };

export interface RuneFieldChangeDetail {
    value: string;
}

@customElement("rune-field")
export class RuneField extends LitElement {
    static formAssociated = true;

    @property({ reflect: true }) type: RuneFieldType = "text";
    @property() label = "";
    @property() hint = "";
    @property() placeholder = "";
    @property() value = "";
    @property() name = "";
    @property({ type: Boolean, reflect: true }) disabled = false;
    @property({ type: Boolean, reflect: true }) invalid = false;
    @property({ attribute: false }) validate?: RuneFieldValidator;

    @state() private _error: string | null = null;

    @query("input") private _input!: HTMLInputElement;

    private _internals: ElementInternals;

    constructor() {
        super();
        this._internals = this.attachInternals();
    }

    connectedCallback() {
        super.connectedCallback();
        this._internals.setFormValue(this.value);
        this.#runValidator();
    }

    formAssociatedCallback() {
        this._internals.setFormValue(this.value);
    }

    updated(changed: Map<string, unknown>) {
        if (changed.has("value")) {
            this._internals.setFormValue(this.value);
        }
        if (changed.has("validate")) {
            this.#runValidator();
        }
    }

    static styles = [
        ...sharedStyles,
        css`
            :host {
                display: block;
                font-family: var(--font-mono);
            }

            label {
                display: block;
                font-size: var(--fs-caption);
                color: var(--text-muted);
                letter-spacing: 0.08em;
                text-transform: uppercase;
                margin-bottom: var(--space-1);
            }

            .control {
                display: flex;
                align-items: stretch;
                background: var(--stone-edge);
                border-radius: var(--radius-sm);
                box-shadow: inset 0 1px 0 var(--stone-bevel);
                transition:
                    background var(--motion-fast),
                    box-shadow var(--motion-reveal);
            }
            ::slotted([slot="leading"]) {
                align-self: center;
                margin-left: var(--space-3);
            }
            ::slotted([slot="trailing"]) {
                align-self: center;
                margin-right: var(--space-3);
            }
            .control:hover {
                background: color-mix(in srgb, var(--stone-edge) 85%, var(--rune-soft));
            }
            :host([invalid]) .control {
                box-shadow:
                    inset 0 1px 0 var(--stone-bevel),
                    0 0 0 1px var(--state-fail);
            }
            :host(:focus-within) .control {
                box-shadow:
                    inset 0 1px 0 var(--stone-bevel),
                    0 0 0 var(--focus-ring-width) var(--focus-ring);
                background: color-mix(in srgb, var(--stone-edge) 70%, var(--rune-soft));
            }
            :host([disabled]) .control {
                opacity: var(--feedback-disabled);
                pointer-events: none;
            }

            input {
                flex: 1;
                min-width: 0;
                padding: var(--space-2) var(--space-3);
                font: inherit;
                color: var(--text-strong);
                background: transparent;
                font-variant-numeric: tabular-nums;
                letter-spacing: 0.02em;
            }
            input:focus,
            input:focus-visible {
                outline: none;
            }
            input::placeholder {
                color: var(--text-faint);
            }

            .hint {
                font-size: var(--fs-caption);
                color: var(--text-faint);
                margin-top: var(--space-1);
                letter-spacing: 0.02em;
            }
            :host([invalid]) .hint {
                color: var(--state-fail);
            }
        `,
    ];

    render() {
        const hintText = this._error ?? this.hint;
        const hasHintSlot = hintText || this._hasNamedSlot("hint");
        return html`
            ${this.label
                ? html`<label part="label" for="input">${this.label}</label>`
                : null}
            <div class="control" part="control">
                <slot name="leading"></slot>
                <input
                    id="input"
                    type="text"
                    inputmode=${this.type === "number" ? "decimal" : "text"}
                    .value=${this.value}
                    placeholder=${this.placeholder}
                    ?disabled=${this.disabled}
                    @input=${this.#onInput}
                    @change=${this.#onChange}
                    part="input"
                />
                <slot name="trailing"></slot>
            </div>
            ${hasHintSlot
                ? html`<div class="hint" part="hint">
                      <slot name="hint">${hintText}</slot>
                  </div>`
                : null}
        `;
    }

    #onInput = (e: Event) => {
        this.value = (e.target as HTMLInputElement).value;
        this._internals.setFormValue(this.value);
        this.#runValidator();
    };

    #runValidator() {
        if (!this.validate) {
            this._error = null;
            return;
        }
        const err = this.validate(this.value);
        this._error = err;
        this.invalid = err !== null;
    }

    #onChange = () => {
        const detail: RuneFieldChangeDetail = { value: this.value };
        this.dispatchEvent(new CustomEvent("change", {
            bubbles: true,
            composed: true,
            detail,
        }));
    };

    private _hasNamedSlot(name: string): boolean {
        return Array.from(this.children).some((c) => c.getAttribute("slot") === name);
    }

    focus(opts?: FocusOptions) {
        this._input?.focus(opts);
    }
}

declare global {
    interface HTMLElementTagNameMap {
        "rune-field": RuneField;
    }
}
