/**
 * Disclosure primitive — wraps native <details>/<summary>. Animates
 * height via `interpolate-size: allow-keywords` (modern CSS, no JS).
 *
 * HIG: https://developer.apple.com/design/human-interface-guidelines/disclosure-controls
 */

import { LitElement, css, html } from "lit";
import { customElement, property, query } from "lit/decorators.js";
import { sharedStyles } from "./_base";

@customElement("rune-disclosure")
export class RuneDisclosure extends LitElement {
    @property({ type: Boolean, reflect: true }) open = false;

    @query("details") private _details!: HTMLDetailsElement;

    static styles = [
        ...sharedStyles,
        css`
            :host { display: block; interpolate-size: allow-keywords; }

            details {
                background: var(--rune-disclosure-bg, transparent);
                border-radius: var(--radius-md);
            }

            summary {
                display: flex;
                align-items: center;
                gap: var(--space-2);
                padding: var(--space-2) var(--space-3);
                font-family: var(--font-mono);
                font-size: var(--fs-caption);
                letter-spacing: 0.06em;
                text-transform: uppercase;
                color: var(--text-muted);
                cursor: pointer;
                list-style: none;
                border-radius: var(--radius-md);
                transition: color var(--motion-fast), background var(--motion-fast);
            }
            summary::-webkit-details-marker { display: none; }
            summary::marker { content: ""; }

            summary:hover { color: var(--text); background: var(--feedback-hover); }

            .chevron {
                display: inline-block;
                width: 10px;
                height: 10px;
                border-right: 1.5px solid currentColor;
                border-bottom: 1.5px solid currentColor;
                transform: rotate(-45deg);
                transition: transform var(--motion-reveal);
            }
            details[open] .chevron { transform: rotate(45deg); }

            .body {
                overflow: hidden;
                padding: var(--space-3);
            }

            /* height animation via interpolate-size */
            details::details-content {
                block-size: 0;
                opacity: 0;
                overflow: clip;
                transition:
                    block-size var(--motion-reveal),
                    opacity var(--motion-reveal),
                    content-visibility var(--motion-reveal) allow-discrete;
            }
            details[open]::details-content {
                block-size: auto;
                opacity: 1;
            }
        `,
    ];

    render() {
        return html`
            <details
                ?open=${this.open}
                @toggle=${this.#onToggle}
            >
                <summary part="summary">
                    <span class="chevron" aria-hidden="true"></span>
                    <slot name="summary">Details</slot>
                </summary>
                <div class="body" part="body">
                    <slot></slot>
                </div>
            </details>
        `;
    }

    #onToggle = () => {
        const wasOpen = this.open;
        this.open = this._details.open;
        if (this.open === wasOpen) return;
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
