import { LitElement, css, html } from "lit";
import { customElement, state } from "lit/decorators.js";
import { gsap } from "gsap";
import "./ritual-dial";
import "./dial-telemetry";
import { formatEta } from "./telemetry-format";
import type { DialGlyph, DialState } from "./ritual-dial";

const TOTAL_BYTES = 980 * 1024 * 1024;
const TRANSFER_S = 6;
const HOLD_S = 1.4;
const ETA_CADENCE_MS = 500;
const EWMA_ALPHA = 0.18;

function snapEta(secs: number): number {
    if (secs < 60) return Math.round(secs);
    if (secs < 600) return Math.round(secs / 10) * 10;
    return Math.round(secs / 60) * 60;
}

@customElement("dial-composition-cycle")
export class DialCompositionCycle extends LitElement {
    @state() private state: DialState = "idle";
    @state() private arc = 0;
    @state() private glyph: DialGlyph = "play";
    @state() private label = "Start";
    @state() private sub = "";
    @state() private bytesDone = 0;
    @state() private speedBps = 0;
    @state() private etaSeconds: number | null = null;
    @state() private showTelemetry = false;

    private tl?: gsap.core.Timeline;
    private rafId = 0;
    private lastEtaAt = 0;
    private lastSampleAt = 0;
    private lastBytes = 0;

    private resetTransfer() {
        this.bytesDone = 0;
        this.speedBps = 0;
        this.etaSeconds = null;
        this.lastBytes = 0;
        this.lastSampleAt = performance.now();
        this.lastEtaAt = performance.now();
    }

    private isTransferring(): boolean {
        return this.state === "prep" || this.state === "final";
    }

    connectedCallback() {
        super.connectedCallback();
        const a = { v: 0 };
        const driveProgress = () => {
            this.arc = a.v;
            this.bytesDone = Math.floor(TOTAL_BYTES * a.v);
        };
        const tl = gsap.timeline({ repeat: -1 });
        tl.call(() => {
            this.state = "idle"; this.arc = 0; this.glyph = "play";
            this.label = "Start"; this.sub = "";
            this.showTelemetry = false;
        });
        tl.to({}, { duration: HOLD_S });
        tl.call(() => {
            this.state = "prep"; this.glyph = "download";
            this.label = "Getting ready"; this.sub = formatEta(null);
            this.showTelemetry = true;
            a.v = 0;
            this.resetTransfer();
        });
        tl.to(a, { v: 1, duration: TRANSFER_S, ease: "power1.inOut", onUpdate: driveProgress });
        tl.call(() => {
            this.state = "run"; this.glyph = "stop";
            this.label = "Ready to play"; this.sub = "Hold to stop";
            this.showTelemetry = false;
            this.arc = 1;
        });
        tl.to({}, { duration: HOLD_S });
        tl.call(() => {
            this.state = "final"; this.glyph = "upload";
            this.label = "Saving"; this.sub = formatEta(null);
            this.showTelemetry = true;
            a.v = 0;
            this.resetTransfer();
        });
        tl.to(a, { v: 1, duration: TRANSFER_S, ease: "power1.inOut", onUpdate: driveProgress });
        tl.to({}, { duration: 0.4 });
        this.tl = tl;
        this.rafId = requestAnimationFrame(this.sampleLoop);
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        this.tl?.kill();
        cancelAnimationFrame(this.rafId);
    }

    private sampleLoop = () => {
        const now = performance.now();
        const transferring = this.isTransferring();
        const dt = now - this.lastSampleAt;
        if (dt >= 80 && transferring) {
            const jitter = 0.7 + Math.random() * 0.6;
            const inst = Math.max(0, ((this.bytesDone - this.lastBytes) / dt) * 1000 * jitter);
            this.speedBps = this.speedBps === 0
                ? inst
                : this.speedBps + EWMA_ALPHA * (inst - this.speedBps);
            this.lastBytes = this.bytesDone;
            this.lastSampleAt = now;
        }
        if (transferring && now - this.lastEtaAt >= ETA_CADENCE_MS) {
            const remaining = TOTAL_BYTES - this.bytesDone;
            this.etaSeconds = this.speedBps > 0 ? snapEta(remaining / this.speedBps) : null;
            this.sub = formatEta(this.etaSeconds);
            this.lastEtaAt = now;
        }
        this.rafId = requestAnimationFrame(this.sampleLoop);
    };

    render() {
        return html`
            <div class="frame">
                <ritual-dial
                    .state=${this.state}
                    .arc=${this.arc}
                    .glyph=${this.glyph}
                    .label=${this.label}
                    .sub=${this.sub}
                ></ritual-dial>
                <div class="telemetry-slot" ?data-shown=${this.showTelemetry}>
                    <dial-telemetry
                        .speedBps=${this.speedBps}
                        .bytesDone=${this.bytesDone}
                        .bytesTotal=${TOTAL_BYTES}
                    ></dial-telemetry>
                </div>
            </div>
        `;
    }

    static styles = css`
        .frame {
            display: flex;
            flex-direction: column;
            align-items: center;
            gap: 1.25rem;
            padding: 1.5rem;
        }
        .telemetry-slot {
            opacity: 0;
            transform: translateY(-4px);
            transition: opacity 240ms ease, transform 240ms ease;
            min-height: 1.5rem;
        }
        .telemetry-slot[data-shown] {
            opacity: 1;
            transform: translateY(0);
        }
    `;
}

interface Args {
    state: DialState;
    arc: number;
    label: string;
    sub: string;
    glyph: DialGlyph;
    speedBps: number;
    bytesDone: number;
    bytesTotal: number;
    showTelemetry: boolean;
}

export default {
    title: "Dial / Composition",
    argTypes: {
        state: { control: { type: "select" }, options: ["idle", "prep", "run", "final", "fail"] },
        arc: { control: { type: "range", min: 0, max: 1, step: 0.01 } },
        label: { control: { type: "text" } },
        sub: { control: { type: "text" } },
        glyph: { control: { type: "select" }, options: ["play", "stop", "x", "download", "upload"] },
        speedBps: { control: { type: "range", min: 0, max: 200 * 1024 * 1024, step: 1024 } },
        bytesDone: { control: { type: "range", min: 0, max: 1024 * 1024 * 1024, step: 1024 * 1024 } },
        bytesTotal: { control: { type: "range", min: 0, max: 1024 * 1024 * 1024, step: 1024 * 1024 } },
        showTelemetry: { control: { type: "boolean" } },
    },
    args: {
        state: "prep",
        arc: 0.42,
        label: "Getting ready",
        sub: "00:13",
        glyph: "download",
        speedBps: 42 * 1024 * 1024,
        bytesDone: 412 * 1024 * 1024,
        bytesTotal: 980 * 1024 * 1024,
        showTelemetry: true,
    },
};

export const Playground = (a: Args) => html`
    <div style="display:flex; flex-direction:column; align-items:center; gap:1.25rem; padding:1.5rem;">
        <ritual-dial
            .state=${a.state}
            .arc=${a.arc}
            .glyph=${a.glyph}
            .label=${a.label}
            .sub=${a.sub}
        ></ritual-dial>
        <dial-telemetry
            style=${a.showTelemetry ? "" : "visibility:hidden"}
            .speedBps=${a.speedBps}
            .bytesDone=${a.bytesDone}
            .bytesTotal=${a.bytesTotal}
        ></dial-telemetry>
    </div>
`;

export const Cycle = () => html`<dial-composition-cycle></dial-composition-cycle>`;
