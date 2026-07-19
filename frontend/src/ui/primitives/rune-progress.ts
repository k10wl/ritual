/**
 * Progress primitive — circular ring or linear bar. Value 0–1 (omit / NaN
 * for indeterminate). Color via `--rune-progress-color` from the parent.
 *
 * HIG: https://developer.apple.com/design/human-interface-guidelines/progress-indicators
 */

import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import { sharedStyles } from "./_base";

export type RuneProgressVariant = "ring" | "linear";

@customElement("rune-progress")
export class RuneProgress extends LitElement {
    @property({ reflect: true }) variant: RuneProgressVariant = "ring";
    @property({ type: Number }) value: number | null = null;
    @property({ type: Number }) size = 24;

    static styles = [
        ...sharedStyles,
        css`
            :host {
                display: inline-block;
                color: var(--rune-progress-color, var(--rune));
            }

            /* ring */
            :host([variant="ring"]) {
                width: var(--rune-progress-size, 24px);
                height: var(--rune-progress-size, 24px);
            }
            .ring {
                width: 100%;
                height: 100%;
                transform: rotate(-90deg);
            }
            .ring .track {
                stroke: var(--dial-track);
                fill: none;
            }
            .ring .arc {
                stroke: currentColor;
                fill: none;
                stroke-linecap: round;
                transition: stroke-dashoffset var(--motion-settle);
            }
            .ring.indeterminate .arc {
                animation: spin 1.4s linear infinite;
                transform-origin: 50% 50%;
            }
            @keyframes spin {
                0%   { stroke-dashoffset: 240; }
                50%  { stroke-dashoffset: 60;  }
                100% { stroke-dashoffset: 240; }
            }

            /* linear */
            :host([variant="linear"]) {
                display: block;
                width: 100%;
                height: var(--rune-progress-size, 4px);
                background: var(--dial-track);
                border-radius: 999px;
                overflow: hidden;
            }
            .bar {
                height: 100%;
                background: currentColor;
                transform-origin: 0 50%;
                transition: transform var(--motion-settle);
            }
            .bar.indeterminate {
                animation: slide 1.6s ease-in-out infinite;
                transform: scaleX(0.4);
            }
            @keyframes slide {
                0%   { transform: translateX(-100%) scaleX(0.4); }
                60%  { transform: translateX(120%)  scaleX(0.4); }
                100% { transform: translateX(120%)  scaleX(0.4); }
            }
        `,
    ];

    render() {
        const indeterminate = this.value === null || Number.isNaN(this.value);
        const v = indeterminate ? 0 : Math.max(0, Math.min(1, this.value as number));

        if (this.variant === "linear") {
            const scale = indeterminate ? undefined : `scaleX(${v})`;
            return html`
                <div
                    class=${"bar" + (indeterminate ? " indeterminate" : "")}
                    style=${scale ? `transform: ${scale};` : ""}
                    part="bar"
                    role="progressbar"
                    aria-valuemin="0"
                    aria-valuemax="1"
                    aria-valuenow=${indeterminate ? "" : String(v)}
                ></div>
            `;
        }

        // ring
        const stroke = 3;
        const r = 50 - stroke / 2;
        const circumference = 2 * Math.PI * r;
        const dashoffset = indeterminate ? 0 : circumference * (1 - v);
        return html`
            <svg
                class=${"ring" + (indeterminate ? " indeterminate" : "")}
                viewBox="0 0 100 100"
                role="progressbar"
                aria-valuemin="0"
                aria-valuemax="1"
                aria-valuenow=${indeterminate ? "" : String(v)}
                part="ring"
            >
                <circle class="track" cx="50" cy="50" r=${r} stroke-width=${stroke}></circle>
                <circle
                    class="arc"
                    cx="50"
                    cy="50"
                    r=${r}
                    stroke-width=${stroke}
                    stroke-dasharray=${circumference}
                    stroke-dashoffset=${dashoffset}
                ></circle>
            </svg>
        `;
    }
}

declare global {
    interface HTMLElementTagNameMap {
        "rune-progress": RuneProgress;
    }
}
