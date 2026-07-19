/**
 * Button primitive — single element, attribute-driven variants. Emits `press`
 * (not `click`) per the composition rules. Keyboard activation handled by the
 * native <button>.
 *
 * HIG: https://developer.apple.com/design/human-interface-guidelines/buttons
 */

import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import { sharedStyles } from "./_base";

export type RuneButtonVariant = "primary" | "tinted" | "plain";
export type RuneButtonSize = "sm" | "md" | "lg";
export type PressOrigin = "pointer" | "keyboard";

export interface RuneButtonPressDetail {
    origin: PressOrigin;
}

@customElement("rune-button")
export class RuneButton extends LitElement {
    @property({ reflect: true }) variant: RuneButtonVariant = "tinted";
    @property({ reflect: true }) size: RuneButtonSize = "md";
    @property({ type: Boolean, reflect: true }) disabled = false;
    @property({ type: Boolean, reflect: true }) loading = false;

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

            button {
                display: inline-flex;
                align-items: center;
                justify-content: center;
                gap: var(--space-2);
                width: 100%;
                font-family: var(--font-mono);
                letter-spacing: 0.04em;
                cursor: pointer;
                border-radius: var(--rune-button-radius, var(--radius-md));
                background: var(--rune-button-bg);
                color: var(--rune-button-fg);
                box-shadow: var(--rune-button-shadow, inset 0 1px 0 var(--stone-bevel));
                transition:
                    background var(--motion-press),
                    box-shadow var(--motion-press),
                    transform var(--motion-press);
            }
            button:hover {
                background: var(--rune-button-bg-hover, var(--feedback-hover));
            }
            button:active {
                background: var(--rune-button-bg-pressed, var(--feedback-pressed));
                transform: translateY(1px);
            }

            /* size */
            :host([size="sm"]) button { padding: var(--space-1) var(--space-3); font-size: var(--fs-caption); min-height: 28px; }
            :host([size="md"]) button { padding: var(--space-2) var(--space-4); font-size: var(--fs-body);    min-height: 36px; }
            :host([size="lg"]) button { padding: var(--space-3) var(--space-5); font-size: var(--fs-body);    min-height: 44px; }

            /* variant: primary — filled, rune-glow */
            :host([variant="primary"]) {
                --rune-button-bg: color-mix(in srgb, var(--rune) 75%, var(--stone-base));
                --rune-button-fg: var(--text-strong);
                --rune-button-shadow:
                    inset 0 1px 0 var(--rune-hi),
                    0 0 18px -4px var(--rune-soft);
                --rune-button-bg-hover: color-mix(in srgb, var(--rune-hi) 85%, var(--stone-base));
                --rune-button-bg-pressed: color-mix(in srgb, var(--rune) 95%, var(--stone-deep));
            }

            /* variant: tinted — recessed stone */
            :host([variant="tinted"]) {
                --rune-button-bg: var(--stone-edge);
                --rune-button-fg: var(--text);
                --rune-button-shadow: inset 0 1px 0 var(--stone-bevel);
                --rune-button-bg-hover: color-mix(in srgb, var(--stone-edge) 80%, var(--rune-soft));
                --rune-button-bg-pressed: color-mix(in srgb, var(--stone-edge) 60%, var(--rune));
            }

            /* variant: plain — no background until hover */
            :host([variant="plain"]) {
                --rune-button-bg: transparent;
                --rune-button-fg: var(--text-muted);
                --rune-button-shadow: none;
                --rune-button-bg-hover: var(--feedback-hover);
                --rune-button-bg-pressed: var(--feedback-pressed);
            }

            /* loading — dim + caret cursor */
            :host([loading]) button {
                cursor: progress;
                color: var(--text-muted);
            }

            ::slotted(*) {
                pointer-events: none;
            }
        `,
    ];

    render() {
        return html`
            <button
                ?disabled=${this.disabled || this.loading}
                @click=${this.#onClick}
                @keydown=${this.#onKeyDown}
                part="button"
            >
                <slot name="leading"></slot>
                <slot></slot>
                <slot name="trailing"></slot>
            </button>
        `;
    }

    #onClick = (e: MouseEvent) => {
        if (this.disabled || this.loading) return;
        e.stopPropagation();
        this.#emitPress("pointer");
    };

    #onKeyDown = (e: KeyboardEvent) => {
        if (this.disabled || this.loading) return;
        if (e.key !== "Enter" && e.key !== " ") return;
        // <button> already handles activation; intercept only to set origin.
        // Allow native click to fire and re-dispatch as keyboard origin.
        if (e.key === " ") return; // space fires click on release
        if (e.key === "Enter") {
            e.preventDefault();
            this.#emitPress("keyboard");
        }
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
        "rune-button": RuneButton;
    }
}
