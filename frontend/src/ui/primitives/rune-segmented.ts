/**
 * Segmented control primitive — a mutually-exclusive pick over a small set,
 * shown all at once (design-log/033 §Q1). Pure presentation: maps `value` ↔ an
 * option list, emits `change` { value }. No domain knowledge (the retention tier
 * picker and the Local·R2 scope switch both drive it).
 *
 * a11y: `role=radiogroup` with `role=radio` segments + `aria-checked`. Roving
 * tabindex — only the selected segment is a tab stop; ←/→/Home/End move and
 * select (ARIA radiogroup pattern).
 *
 * HIG: https://developer.apple.com/design/human-interface-guidelines/segmented-controls
 */

import { LitElement, css, html } from "lit";
import { customElement, property, queryAll } from "lit/decorators.js";
import { sharedStyles } from "./_base";

export interface SegmentOption {
    value: string;
    label: string;
}

export interface RuneSegmentedChangeDetail {
    value: string;
}

@customElement("rune-segmented")
export class RuneSegmented extends LitElement {
    @property({ attribute: false }) options: SegmentOption[] = [];
    @property() value = "";
    @property({ type: Boolean, reflect: true }) disabled = false;
    /** a11y group label (e.g. the tier name). */
    @property() label: string | null = null;

    @queryAll("[role='radio']") private _segments!: NodeListOf<HTMLElement>;

    static styles = [
        ...sharedStyles,
        css`
            :host {
                display: block;
            }
            :host([disabled]) {
                opacity: var(--feedback-disabled);
                pointer-events: none;
            }
            .group {
                display: inline-grid;
                grid-auto-flow: column;
                grid-auto-columns: 1fr;
                gap: var(--rune-segmented-gap, 2px);
                padding: 2px;
                border-radius: var(--rune-segmented-radius, var(--radius-md));
                background: var(--rune-segmented-track, var(--stone-deep));
                box-shadow: inset 0 1px 0 var(--stone-bevel);
            }
            .seg {
                font-family: var(--font-mono);
                font-size: var(--fs-body);
                min-width: var(--rune-segmented-min, 2.2em);
                padding: var(--space-1) var(--space-3);
                text-align: center;
                cursor: pointer;
                color: var(--text-muted);
                border-radius: calc(var(--rune-segmented-radius, var(--radius-md)) - 2px);
                transition:
                    background var(--motion-fast),
                    color var(--motion-fast);
            }
            .seg:hover {
                color: var(--text);
            }
            .seg[aria-checked="true"] {
                background: var(--rune-segmented-fill, var(--stone-raised, #2c3a4f));
                color: var(--text-strong);
                box-shadow: 0 1px 2px rgb(0 0 0 / 0.25);
            }
            .seg:focus-visible {
                outline: none;
                box-shadow: 0 0 0 var(--focus-ring-width) var(--focus-ring);
            }
        `,
    ];

    render() {
        return html`<div
            class="group"
            role="radiogroup"
            aria-label=${this.label ?? "Choose one"}
        >
            ${this.options.map((opt) => {
                const selected = opt.value === this.value;
                return html`<div
                    class="seg"
                    role="radio"
                    aria-checked=${selected ? "true" : "false"}
                    aria-label=${opt.label}
                    tabindex=${selected ? 0 : -1}
                    data-value=${opt.value}
                    @click=${() => this.#select(opt.value)}
                    @keydown=${this.#onKeydown}
                >
                    ${opt.label}
                </div>`;
            })}
        </div>`;
    }

    #select(value: string) {
        if (value === this.value) return;
        this.value = value;
        this.dispatchEvent(
            new CustomEvent<RuneSegmentedChangeDetail>("change", {
                detail: { value },
                bubbles: true,
                composed: true,
            }),
        );
    }

    #onKeydown = (e: KeyboardEvent) => {
        const values = this.options.map((o) => o.value);
        const i = values.indexOf(this.value);
        let next = -1;
        switch (e.key) {
            case "ArrowRight":
            case "ArrowDown":
                next = (i + 1) % values.length;
                break;
            case "ArrowLeft":
            case "ArrowUp":
                next = (i - 1 + values.length) % values.length;
                break;
            case "Home":
                next = 0;
                break;
            case "End":
                next = values.length - 1;
                break;
            default:
                return;
        }
        e.preventDefault();
        this.#select(values[next]);
        // Move focus to follow selection (roving tabindex).
        void this.updateComplete.then(() => this._segments[next]?.focus());
    };
}

declare global {
    interface HTMLElementTagNameMap {
        "rune-segmented": RuneSegmented;
    }
}
