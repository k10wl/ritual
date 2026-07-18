/**
 * Disclosure primitive — a summary button + a collapsible region.
 *
 * Motion: smooth height grow (`grid-template-rows: 0fr → 1fr`) with a per-child
 * fade + scan-line drop on the slotted body — children stagger one after
 * another (forward on open, reverse on close). Summary stays put.
 *
 * Caveat: the stagger targets *direct* slotted nodes via `::slotted(*)`. If a
 * caller wraps all body content in a single container (e.g. a `<form>`), the
 * stagger collapses to one block fade. Pass sibling children to keep the
 * cascade visible.
 *
 * `grid-template-rows: 0fr → 1fr` works in every current WebKit/Chromium/Firefox
 * without experimental flags, unlike `::details-content` + `interpolate-size`
 * (see design log #015).
 *
 * HIG: https://developer.apple.com/design/human-interface-guidelines/disclosure-controls
 */

import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import { sharedStyles } from "./_base";

@customElement("rune-disclosure")
export class RuneDisclosure extends LitElement {
    @property({ type: Boolean, reflect: true }) open = false;

    static styles = [
        ...sharedStyles,
        css`
            :host { display: block; }

            .summary {
                width: 100%;
                display: flex;
                align-items: center;
                gap: var(--space-2);
                padding: var(--space-2) var(--space-3);
                margin: 0;
                background: var(--rune-disclosure-bg, transparent);
                border: 0;
                border-radius: var(--radius-md);
                font: inherit;
                font-family: var(--font-mono);
                font-size: var(--fs-caption);
                letter-spacing: 0.06em;
                text-transform: uppercase;
                color: var(--text-muted);
                text-align: left;
                cursor: pointer;
                transition: color var(--motion-fast), background var(--motion-fast);
            }
            .summary:hover { color: var(--text); background: var(--feedback-hover); }
            .summary:focus-visible {
                outline: 2px solid var(--focus-ring, currentColor);
                outline-offset: 2px;
            }

            .chevron {
                display: inline-block;
                width: 12px;
                height: 12px;
                color: currentColor;
                transform: rotate(-90deg);
                transition: transform var(--motion-base);
            }
            :host([open]) .chevron { transform: rotate(0deg); }
            .chevron svg { display: block; width: 100%; height: 100%; }

            /* Smooth side: height grows with a deliberate ease (--motion-settle, 320ms). */
            .body-wrap {
                display: grid;
                grid-template-rows: 0fr;
                transition: grid-template-rows var(--motion-settle);
            }
            :host([open]) .body-wrap { grid-template-rows: 1fr; }

            .body {
                min-height: 0;
                overflow: hidden;
            }

            .body-inner { padding: var(--space-3); }

            /* Body cascade: each default-slot child fades + drops in turn.
               Scoped to :not([slot]) so the summary span never participates
               (it must always stay visible). nth-child(N of :not([slot]))
               keeps the index correct whether or not a summary is slotted. */
            ::slotted(:not([slot])) {
                opacity: 0;
                transform: translateY(-4px);
                transition:
                    opacity var(--motion-base),
                    transform var(--motion-base);
            }
            /* Close: all children fade together — no cascade on exit. */
            :host([open]) ::slotted(:not([slot])) {
                opacity: 1;
                transform: translateY(0);
            }
            /* Open cascade: top child arrives first, after the height starts. */
            :host([open]) ::slotted(:nth-child(1 of :not([slot]))) { transition-delay:  60ms; }
            :host([open]) ::slotted(:nth-child(2 of :not([slot]))) { transition-delay: 110ms; }
            :host([open]) ::slotted(:nth-child(3 of :not([slot]))) { transition-delay: 160ms; }
            :host([open]) ::slotted(:nth-child(4 of :not([slot]))) { transition-delay: 210ms; }
            :host([open]) ::slotted(:nth-child(5 of :not([slot]))) { transition-delay: 260ms; }
            :host([open]) ::slotted(:nth-child(n+6 of :not([slot]))) { transition-delay: 310ms; }

            @media (prefers-reduced-motion: reduce) {
                .chevron, .body-wrap { transition: none; }
                ::slotted(:not([slot])) { transition: none; }
            }
        `,
    ];

    render() {
        return html`
            <button
                type="button"
                class="summary"
                part="summary"
                aria-expanded=${this.open ? "true" : "false"}
                aria-controls="body"
                @click=${this.#toggle}
            >
                <span class="chevron" aria-hidden="true">
                    <svg viewBox="0 0 12 12" fill="none" stroke="currentColor"
                         stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">
                        <polyline points="3,4.5 6,7.5 9,4.5"></polyline>
                    </svg>
                </span>
                <slot name="summary">Details</slot>
            </button>
            <div class="body-wrap" id="body" role="region" part="body">
                <div class="body">
                    <div class="body-inner">
                        <slot></slot>
                    </div>
                </div>
            </div>
        `;
    }

    #toggle = () => {
        this.open = !this.open;
        this.dispatchEvent(new CustomEvent(this.open ? "open" : "close", {
            bubbles: true,
            composed: true,
        }));
    };
}

declare global {
    interface HTMLElementTagNameMap {
        "rune-disclosure": RuneDisclosure;
    }
}
