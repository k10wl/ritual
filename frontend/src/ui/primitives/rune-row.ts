/**
 * List row primitive — HIG list-table whole-row pressable. Layout slots:
 * leading / default / trailing. When `pressable`, emits `press` on click +
 * Enter/Space; the row acts as a button with proper a11y role/tabindex.
 *
 * For external layout overrides (grid template, custom states), use the
 * `::part(container)` part.
 *
 * HIG: https://developer.apple.com/design/human-interface-guidelines/lists-and-tables
 */

import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import { sharedStyles } from "./_base";
import type { PressOrigin, RuneButtonPressDetail } from "./rune-button";

@customElement("rune-row")
export class RuneRow extends LitElement {
    @property({ type: Boolean, reflect: true }) pressable = false;
    @property({ type: Boolean, reflect: true }) active = false;
    @property({ type: Boolean, reflect: true }) disabled = false;
    @property() ariaLabel: string | null = null;

    static styles = [
        ...sharedStyles,
        css`
            :host {
                display: block;
                font-family: var(--font-mono);
            }

            .container {
                display: grid;
                grid-template-columns: var(--rune-row-template, auto 1fr auto);
                align-items: center;
                gap: var(--rune-row-gap, var(--space-3));
                padding: var(--rune-row-padding, var(--space-2) var(--space-3));
                min-height: var(--rune-row-min-height, 32px);
                background: var(--rune-row-bg, transparent);
                color: inherit;
                border-radius: var(--radius-sm);
                box-shadow: var(--rune-row-shadow, none);
                transition:
                    background var(--motion-fast),
                    box-shadow var(--motion-reveal);
            }

            :host([pressable]) .container {
                cursor: pointer;
                outline: none;
            }
            :host([pressable]) .container:hover {
                background: var(--rune-row-bg-hover, var(--feedback-hover));
            }
            :host([pressable]) .container:active {
                background: var(--rune-row-bg-pressed, var(--feedback-pressed));
            }
            :host([pressable]) .container:focus-visible {
                box-shadow: 0 0 0 var(--focus-ring-width) var(--focus-ring);
            }

            :host([active]) .container {
                background: var(--rune-row-bg-active, color-mix(in srgb, var(--rune) 20%, transparent));
                box-shadow: var(--rune-row-shadow-active, 0 0 0 1px var(--rune-soft));
            }

            :host([disabled]) .container {
                opacity: var(--feedback-disabled);
                pointer-events: none;
            }

            ::slotted([slot="leading"]),
            ::slotted([slot="trailing"]) {
                display: inline-flex;
                align-items: center;
                pointer-events: none;
            }
        `,
    ];

    render() {
        const pressable = this.pressable && !this.disabled;
        return html`
            <div
                class="container"
                part="container"
                role=${pressable ? "button" : "row"}
                tabindex=${pressable ? "0" : "-1"}
                aria-label=${this.ariaLabel ?? ""}
                aria-disabled=${this.disabled ? "true" : "false"}
                @click=${this.#onClick}
                @keydown=${this.#onKeyDown}
            >
                <slot name="leading"></slot>
                <slot></slot>
                <slot name="trailing"></slot>
            </div>
        `;
    }

    #onClick = () => {
        if (!this.pressable || this.disabled) return;
        this.#emitPress("pointer");
    };

    #onKeyDown = (e: KeyboardEvent) => {
        if (!this.pressable || this.disabled) return;
        if (e.key !== "Enter" && e.key !== " ") return;
        e.preventDefault();
        this.#emitPress("keyboard");
    };

    #emitPress(origin: PressOrigin) {
        const detail: RuneButtonPressDetail = { origin };
        this.dispatchEvent(new CustomEvent("press", {
            bubbles: true,
            composed: true,
            detail,
        }));
    }
}

declare global {
    interface HTMLElementTagNameMap {
        "rune-row": RuneRow;
    }
}
