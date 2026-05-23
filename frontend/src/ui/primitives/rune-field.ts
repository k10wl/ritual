/**
 * Labelled input primitive — HIG label-above text-field pattern. Form-associated
 * custom element so values participate in <form> submission.
 *
 * HIG: https://developer.apple.com/design/human-interface-guidelines/text-fields
 */

import { LitElement, css, html } from "lit";
import { customElement, property, query } from "lit/decorators.js";
import { sharedStyles } from "./_base";

export type RuneFieldType = "text" | "number";

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
    @property() min = "";
    @property() max = "";
    @property() step = "";
    @property({ type: Boolean, reflect: true }) disabled = false;
    @property({ type: Boolean, reflect: true }) invalid = false;

    @query("input") private _input!: HTMLInputElement;

    private _internals: ElementInternals;

    constructor() {
        super();
        this._internals = this.attachInternals();
    }

    connectedCallback() {
        super.connectedCallback();
        this._internals.setFormValue(this.value);
    }

    formAssociatedCallback() {
        this._internals.setFormValue(this.value);
    }

    updated(changed: Map<string, unknown>) {
        if (changed.has("value")) {
            this._internals.setFormValue(this.value);
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
                align-items: center;
                gap: var(--space-2);
                padding: var(--space-2) var(--space-3);
                background: var(--stone-edge);
                border-radius: var(--radius-sm);
                box-shadow: inset 0 1px 0 var(--stone-bevel);
                transition:
                    background var(--motion-fast),
                    box-shadow var(--motion-reveal);
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
                font: inherit;
                color: var(--text-strong);
                background: transparent;
                font-variant-numeric: tabular-nums;
                letter-spacing: 0.02em;
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
        const hasHintSlot = this.hint || this._hasNamedSlot("hint");
        return html`
            ${this.label
                ? html`<label part="label" for="input">${this.label}</label>`
                : null}
            <div class="control" part="control">
                <slot name="leading"></slot>
                <input
                    id="input"
                    type=${this.type}
                    .value=${this.value}
                    placeholder=${this.placeholder}
                    min=${this.min || ""}
                    max=${this.max || ""}
                    step=${this.step || ""}
                    ?disabled=${this.disabled}
                    @input=${this.#onInput}
                    @change=${this.#onChange}
                    part="input"
                />
                <slot name="trailing"></slot>
            </div>
            ${hasHintSlot
                ? html`<div class="hint" part="hint">
                      <slot name="hint">${this.hint}</slot>
                  </div>`
                : null}
        `;
    }

    #onInput = (e: Event) => {
        this.value = (e.target as HTMLInputElement).value;
        this._internals.setFormValue(this.value);
    };

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
