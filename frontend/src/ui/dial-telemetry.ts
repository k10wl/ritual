import { LitElement, css, html, TemplateResult } from "lit";
import { customElement, property } from "lit/decorators.js";
import "./stable-num";
import "./decoder-v2";
import { formatSize, formatSpeed } from "./telemetry-format";

const NUMERIC_PLACEHOLDER = "·····";

function jitterStone(text: string, fast: boolean): TemplateResult {
    const idleMin = fast ? 50 : 1800;
    const idleMax = fast ? 120 : 3600;
    const idleRadius = fast ? Math.max(1, text.length) : 1;
    return html`<decoder-v2
        .text=${text}
        .splashRounds=${[3, 5]}
        splash-radius="1"
        splash-tick-ms="22"
        idle-min-ms=${idleMin}
        idle-max-ms=${idleMax}
        idle-radius=${idleRadius}
    ></decoder-v2>`;
}

@customElement("dial-telemetry")
export class DialTelemetry extends LitElement {
    @property({ type: Number }) speedBps = 0;
    @property({ type: Number }) bytesDone = 0;
    @property({ type: Number }) bytesTotal = 0;

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
            gap: 0.15rem;
            font-size: 0.78rem;
            font-weight: 400;
            font-variant-numeric: tabular-nums;
            letter-spacing: 0.04em;
            color: rgba(232, 240, 255, 0.55);
        }
        .row {
            display: inline-flex;
            align-items: baseline;
            gap: 0.3rem;
        }
        .unit {
            opacity: 0.7;
        }
        .sep {
            opacity: 0.5;
        }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        "dial-telemetry": DialTelemetry;
    }
}
