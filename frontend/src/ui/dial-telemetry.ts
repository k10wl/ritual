import { LitElement, css, html, TemplateResult } from "lit";
import { customElement, property } from "lit/decorators.js";
import { gsap } from "gsap";
import "./stable-num";
import "./decoder";
import { formatSize, formatSpeed } from "./telemetry-format";

const NUMERIC_PLACEHOLDER = "·····";
const ROW_ENTER_S = 0.36;
const ROW_EXIT_S = 0.28;
const ROW_STAGGER_S = 0.055;
const ROW_SLIDE_PX = -12;
export const DIAL_TELEMETRY_EXIT_TOTAL_S = ROW_EXIT_S + ROW_STAGGER_S * 2;

const reducedMotion = (): boolean =>
    window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;

function jitterStone(text: string, fast: boolean): TemplateResult {
    const idleMin = fast ? 50 : 1800;
    const idleMax = fast ? 120 : 3600;
    const idleRadius = fast ? Math.max(1, text.length) : 1;
    return html`<rune-decoder
        .text=${text}
        .splashRounds=${[3, 5]}
        splash-radius="1"
        splash-tick-ms="22"
        idle-min-ms=${idleMin}
        idle-max-ms=${idleMax}
        idle-radius=${idleRadius}
    ></rune-decoder>`;
}

@customElement("dial-telemetry")
export class DialTelemetry extends LitElement {
    @property({ type: Number }) speedBps = 0;
    @property({ type: Number }) bytesDone = 0;
    @property({ type: Number }) bytesTotal = 0;

    firstUpdated() {
        this.playEnter();
    }

    private rows(): NodeListOf<Element> | undefined {
        return this.shadowRoot?.querySelectorAll(".row");
    }

    private playEnter() {
        if (reducedMotion()) return;
        const rows = this.rows();
        if (!rows?.length) return;
        gsap.from(rows, {
            y: ROW_SLIDE_PX,
            opacity: 0,
            duration: ROW_ENTER_S,
            ease: "back.out(1.4)",
            stagger: ROW_STAGGER_S,
            overwrite: true,
        });
    }

    playExit(): gsap.core.Tween | undefined {
        if (reducedMotion()) return undefined;
        const rows = this.rows();
        if (!rows?.length) return undefined;
        return gsap.to(rows, {
            y: ROW_SLIDE_PX,
            opacity: 0,
            duration: ROW_EXIT_S,
            ease: "power2.in",
            stagger: { each: ROW_STAGGER_S, from: "end" },
            overwrite: true,
        });
    }

    render() {
        const rushing = this.speedBps <= 0;
        const sp = formatSpeed(this.speedBps);
        const sz = formatSize(this.bytesDone, this.bytesTotal);
        const speedValue = rushing ? jitterStone(NUMERIC_PLACEHOLDER, true) : sp.value;
        const doneValue = rushing ? jitterStone(NUMERIC_PLACEHOLDER, true) : sz.done;
        return html`
            <div class="row">
                <stable-num chars="6" align="right">${speedValue}</stable-num>
                <span class="unit">${jitterStone(sp.unit, false)}</span>
            </div>
            <div class="row">
                <stable-num chars="6" align="right">${doneValue}</stable-num>
                <span class="unit">${jitterStone(sz.unit, false)}</span>
                ${sz.total ? html`
                    <span class="sep">${jitterStone("/", false)}</span>
                    <span>${sz.total}</span>
                    <span class="unit">${jitterStone(sz.unit, false)}</span>
                ` : null}
            </div>
        `;
    }

    static styles = css`
        :host {
            display: inline-flex;
            flex-direction: column;
            align-items: center;
            gap: 2px;
            font-size: var(--fs-2);
            font-variant-numeric: tabular-nums;
            letter-spacing: 0.04em;
            color: var(--text-muted);
        }
        .row {
            display: inline-flex;
            align-items: baseline;
            gap: var(--space-1);
        }
        .unit {
            color: var(--text-faint);
        }
        .sep {
            color: var(--text-faint);
        }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        "dial-telemetry": DialTelemetry;
    }
}
