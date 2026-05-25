/**
 * Labelled input primitive — HIG label-above text-field pattern. Form-associated
 * custom element so values participate in <form> submission.
 *
 * # Validation
 *
 * Domain-free. Callers inject rules via the `validate` JS property
 * (Formik-style); the primitive only runs the function, paints the
 * `invalid` ring, and replaces the hint with the returned message.
 * Compose multiple rules with {@link composeValidators}.
 *
 * # Why caller-injected (not built-in min/max)
 *
 * Earlier revisions encoded type-aware ranges inside the primitive.
 * Rejected as not-SOLID: primitives must not know domain rules; reuse
 * suffers and every screen ends up fighting the built-ins. Rule
 * authorship belongs to the consumer.
 *
 * # Why `type="number"` is a *prop*, not the rendered HTML type
 *
 * `<input type="number">` silently masks bad input as `""` (Chrome /
 * Safari) and blocks valid keystrokes inconsistently across engines.
 * The user can't see what they typed, the validator can't flag it.
 * Internally we always render `<input type="text">` and pick
 * `inputmode` from the prop — mobile keypad without the footguns.
 * Callers still write `type="number"` for intent + a11y signalling.
 *
 * # Why we never block keystrokes
 *
 * Inputs accept every character. No keydown filters, no paste blockers,
 * no masking. Show the typed string verbatim and let the validator
 * explain what's wrong. Honest UX beats hostile UX.
 *
 * HIG: https://developer.apple.com/design/human-interface-guidelines/text-fields
 */

import { LitElement, css, html } from "lit";
import { customElement, property, query, state } from "lit/decorators.js";
import { sharedStyles } from "./_base";

/**
 * Caller-facing intent for the field. Drives `inputmode` (mobile
 * keypad) and a11y; does NOT change the rendered HTML input type
 * (always `text` — see file header for rationale).
 */
export type RuneFieldType = "text" | "number";

/**
 * Validator contract: receive the current string value, return an
 * error message to display, or `null` when the value is acceptable.
 *
 * Validators run synchronously on every input event so consumers
 * reading `.invalid` during the bubbling `input` event see fresh
 * state (no `await updateComplete` needed).
 */
export type RuneFieldValidator = (value: string) => string | null;

/**
 * Chain validators in priority order: returns the first non-null
 * error, or `null` when every rule passes. Order matters — put
 * shape rules (`required`, `numeric`) before semantic rules
 * (`range`, `multipleOf`) so users see the most fundamental
 * problem first.
 *
 * @example
 * composeValidators(required, integer, range(1, 65535))
 */
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
    // True when the consumer has assigned content to the `hint` slot. Tracked
    // via @slotchange so render stays a pure function of reactive state and
    // doesn't walk light-DOM children — design-log/020 §G.
    @state() private _hasHintSlot = false;

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
        const showHint = !!(hintText || this._hasHintSlot);
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
            <div class="hint" part="hint" ?hidden=${!showHint}>
                <slot name="hint" @slotchange=${this.#onHintSlot}>${hintText}</slot>
            </div>
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

    #onHintSlot = (e: Event) => {
        const slot = e.target as HTMLSlotElement;
        this._hasHintSlot = slot.assignedElements().length > 0;
    };

    focus(opts?: FocusOptions) {
        this._input?.focus(opts);
    }
}

declare global {
    interface HTMLElementTagNameMap {
        "rune-field": RuneField;
    }
}
