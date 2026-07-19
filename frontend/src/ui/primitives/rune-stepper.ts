/**
 * Stepper primitive — a compact `−  value  +` control for a small bounded
 * integer (design-log/033 §Q1 redesign). Roomier and simpler than a 6-segment
 * row when the absolute value matters less than nudging it. Pure presentation:
 * holds `value` within `[min, max]`, emits `change` { value }.
 *
 * a11y: the group is a `role="spinbutton"` (aria-valuenow/min/max/text), a single
 * tab stop, driven by ↑/→/↓/←/Home/End; the −/+ buttons are supplementary.
 *
 * HIG: https://developer.apple.com/design/human-interface-guidelines/steppers
 */

import { LitElement, css, html, nothing } from "lit";
import { customElement, property } from "lit/decorators.js";
import { sharedStyles } from "./_base";

export interface RuneStepperChangeDetail {
    value: number;
}

@customElement("rune-stepper")
export class RuneStepper extends LitElement {
    @property({ type: Number }) value = 0;
    @property({ type: Number }) min = 0;
    /** Upper bound; pass Infinity (or omit a finite cap) for an uncapped stepper. */
    @property({ type: Number }) max = 5;
    @property({ type: Boolean, reflect: true }) disabled = false;
    /** a11y label for the value being stepped (e.g. the tier name). */
    @property() label: string | null = null;

    static styles = [
        ...sharedStyles,
        css`
            :host {
                display: inline-block;
            }
            :host([disabled]) {
                opacity: var(--feedback-disabled);
                pointer-events: none;
            }
            .group {
                display: inline-grid;
                grid-auto-flow: column;
                align-items: center;
                gap: 2px;
                padding: 2px;
                border-radius: var(--rune-stepper-radius, var(--radius-md));
                background: var(--rune-stepper-track, var(--stone-deep));
                box-shadow: inset 0 1px 0 var(--stone-bevel);
            }
            .group:focus-visible {
                outline: none;
                box-shadow:
                    inset 0 1px 0 var(--stone-bevel),
                    0 0 0 var(--focus-ring-width) var(--focus-ring);
            }
            button {
                font-family: var(--font-mono);
                width: 1.8em;
                height: 1.8em;
                cursor: pointer;
                color: var(--text-muted);
                border-radius: calc(var(--rune-stepper-radius, var(--radius-md)) - 2px);
                transition:
                    background var(--motion-fast),
                    color var(--motion-fast);
            }
            button:hover:not(:disabled) {
                background: var(--stone-raised, #2c3a4f);
                color: var(--text-strong);
            }
            button:disabled {
                opacity: var(--feedback-disabled);
                cursor: default;
            }
            .value {
                min-width: 1.6em;
                text-align: center;
                font-family: var(--font-mono);
                font-variant-numeric: tabular-nums;
                color: var(--text-strong);
            }
        `,
    ];

    render() {
        return html`<div
            class="group"
            role="spinbutton"
            tabindex=${this.disabled ? -1 : 0}
            aria-valuenow=${this.value}
            aria-valuemin=${this.min}
            aria-valuemax=${Number.isFinite(this.max) ? this.max : nothing}
            aria-label=${this.label ?? "Value"}
            @keydown=${this.#onKeydown}
        >
            <button
                type="button"
                aria-label="Decrease"
                tabindex="-1"
                ?disabled=${this.value <= this.min}
                @click=${() => this.#set(this.value - 1)}
            >
                −
            </button>
            <span class="value">${this.value}</span>
            <button
                type="button"
                aria-label="Increase"
                tabindex="-1"
                ?disabled=${this.value >= this.max}
                @click=${() => this.#set(this.value + 1)}
            >
                +
            </button>
        </div>`;
    }

    #set(next: number) {
        const clamped = Math.max(this.min, Math.min(this.max, next));
        if (clamped === this.value) return;
        this.value = clamped;
        this.dispatchEvent(
            new CustomEvent<RuneStepperChangeDetail>("change", {
                detail: { value: clamped },
                bubbles: true,
                composed: true,
            }),
        );
    }

    #onKeydown = (e: KeyboardEvent) => {
        switch (e.key) {
            case "ArrowUp":
            case "ArrowRight":
                this.#set(this.value + 1);
                break;
            case "ArrowDown":
            case "ArrowLeft":
                this.#set(this.value - 1);
                break;
            case "Home":
                this.#set(this.min);
                break;
            case "End":
                this.#set(this.max);
                break;
            default:
                return;
        }
        e.preventDefault();
    };
}

declare global {
    interface HTMLElementTagNameMap {
        "rune-stepper": RuneStepper;
    }
}
