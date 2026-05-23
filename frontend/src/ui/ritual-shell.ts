import { LitElement, css, html, PropertyValues } from "lit";
import { customElement, property } from "lit/decorators.js";
import { ContextProvider } from "@lit/context";
import { dialStateContext } from "./contexts/dial-state-context";
import type { DialState } from "./ritual-dial";
import "./ambient-footer";

@customElement("ritual-shell")
export class RitualShell extends LitElement {
    @property({ reflect: true }) state: DialState = "idle";
    private dial = new ContextProvider(this, { context: dialStateContext, initialValue: "idle" });

    private relay = (e: Event) => {
        const ce = e as CustomEvent<"logs" | "folder">;
        this.dispatchEvent(new CustomEvent("ambient-action", {
            detail: ce.detail,
            bubbles: true,
            composed: true,
        }));
    };

    willUpdate(changed: PropertyValues) {
        if (changed.has("state")) this.dial.setValue(this.state);
    }

    render() {
        return html`
            <svg class="defs" aria-hidden="true">
                <filter id="halo-goo" x="-15%" y="-15%" width="130%" height="130%" color-interpolation-filters="sRGB">
                    <feTurbulence type="fractalNoise" baseFrequency="0.009 0.013" numOctaves="2" seed="11" result="noise"/>
                    <feDisplacementMap in="SourceGraphic" in2="noise" scale="120" xChannelSelector="R" yChannelSelector="G"/>
                </filter>
            </svg>
            <div class="halo" aria-hidden="true"></div>
            <section class="stage">
                <slot></slot>
            </section>
            <ambient-footer @ambient-action=${this.relay}></ambient-footer>
        `;
    }

    static styles = css`
        :host {
            position: relative;
            display: flex;
            flex-direction: column;
            min-height: 100vh;
            overflow: hidden;
            color: var(--text-strong);
            font-family: var(--font-mono);
            background: var(--stone-deep);

            /* single state hue, three slight variants per blob */
            --c: var(--state-idle);
            --c-tint-hi: color-mix(in oklab, var(--c) 20%, transparent);
            --c-tint:    color-mix(in oklab, var(--c) 15%, transparent);
            --c-tint-lo: color-mix(in oklab, var(--c) 10%, transparent);
            transition: --c var(--motion-slow, 1200ms ease);
        }
        :host([state="idle"])  { --c: var(--state-idle); }
        :host([state="prep"])  { --c: var(--state-prep); }
        :host([state="run"])   { --c: var(--state-run); }
        :host([state="final"]) { --c: var(--state-final); }
        :host([state="fail"])  { --c: var(--state-fail); }

        .defs {
            position: absolute;
            width: 0;
            height: 0;
            pointer-events: none;
        }
        .halo {
            position: absolute;
            inset: 0;
            pointer-events: none;
            z-index: 0;
            background:
                radial-gradient(36vw 32vw at var(--halo-ax) var(--halo-ay),
                    var(--c-tint-hi), transparent 68%),
                radial-gradient(32vw 34vw at var(--halo-bx) var(--halo-by),
                    var(--c-tint-lo), transparent 70%),
                radial-gradient(30vw 30vw at var(--halo-cx) var(--halo-cy),
                    var(--c-tint),    transparent 70%);
            filter: url(#halo-goo) blur(8px);
            /* per-axis independent prime-period animations: motion never
               cycles back to a previous frame (LCM of prime durations is
               enormous) — feels like drift, not a pendulum.
               each blob is also anchored to one edge quadrant so the
               centre stays clear of cloud cover */
            animation:
                ax 23s cubic-bezier(.45,.05,.55,.95) infinite alternate,
                ay 29s cubic-bezier(.4,.15,.6,.85)   infinite alternate,
                bx 31s cubic-bezier(.5,.0,.5,1)      infinite alternate,
                by 37s cubic-bezier(.35,.2,.65,.8)   infinite alternate,
                cx 41s cubic-bezier(.4,.1,.6,.9)     infinite alternate,
                cy 43s cubic-bezier(.5,.05,.5,.95)   infinite alternate;
        }
        /* TL, TR, BL — centres ride the frame edges, so exactly half
           the cloud is in view at any moment. */
        @keyframes ax { from { --halo-ax: -10%; } to { --halo-ax: 10%; } }
        @keyframes ay { from { --halo-ay:  -8%; } to { --halo-ay: 10%; } }
        @keyframes bx { from { --halo-bx:  90%; } to { --halo-bx: 110%; } }
        @keyframes by { from { --halo-by:  -8%; } to { --halo-by: 10%; } }
        @keyframes cx { from { --halo-cx: -10%; } to { --halo-cx: 10%; } }
        @keyframes cy { from { --halo-cy:  90%; } to { --halo-cy: 108%; } }

        @media (prefers-reduced-motion: reduce) {
            .halo { animation: none; }
        }

        .stage,
        ambient-footer {
            position: relative;
            z-index: 1;
        }
        .stage {
            flex: 1;
            display: flex;
            flex-direction: column;
            align-items: center;
            gap: var(--space-5);
            padding: 150px var(--space-4) var(--space-4);
            box-sizing: border-box;
        }
        ::slotted(*) {
            width: 100%;
            max-width: 480px;
        }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        "ritual-shell": RitualShell;
    }
}
