import { LitElement, html } from "lit";
import { customElement, state } from "lit/decorators.js";
import "./dial-telemetry";

const TOTAL = 980 * 1024 * 1024;
const DURATION_MS = 8000;
const TICK_MS = 100;
const EWMA_ALPHA = 0.18;

// Demo-only stand-in for the Go-side formatSize/formatSpeed (design-log/050)
// — the story simulates what the backend would have sent so the Storybook
// controls can still drive raw byte/speed numbers, while <dial-telemetry>
// itself only ever receives ready text, matching production.
const KB = 1024, MB = KB * 1024, GB = MB * 1024;
function unitFor(n: number) {
    if (n >= GB) return { div: GB, suffix: "GB", decimals: 2 };
    if (n >= MB) return { div: MB, suffix: "MB", decimals: 1 };
    if (n >= KB) return { div: KB, suffix: "KB", decimals: 1 };
    return { div: 1, suffix: "B", decimals: 0 };
}
function demoFormatSize(done: number, total: number) {
    if (total <= 0) {
        const u = unitFor(done);
        return { doneText: (done / u.div).toFixed(u.decimals), totalText: "", unit: u.suffix };
    }
    const u = unitFor(total);
    return {
        doneText: (done / u.div).toFixed(u.decimals),
        totalText: (total / u.div).toFixed(u.decimals),
        unit: u.suffix,
    };
}
function demoFormatSpeed(bps: number) {
    if (!Number.isFinite(bps) || bps <= 0) return { text: "0", unit: "B/s" };
    const u = unitFor(bps);
    return { text: (bps / u.div).toFixed(u.decimals), unit: `${u.suffix}/s` };
}

@customElement("dial-telemetry-run-busy")
export class DialTelemetryRunBusy extends LitElement {
    @state() private bytesDone = 0;
    @state() private speedBps = 0;

    private raf = 0;
    private startedAt = 0;
    private lastTickAt = 0;
    private lastBytes = 0;

    connectedCallback() {
        super.connectedCallback();
        this.startedAt = performance.now();
        this.lastTickAt = this.startedAt;
        this.loop();
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        cancelAnimationFrame(this.raf);
    }

    private loop = () => {
        const now = performance.now();
        const dt = now - this.lastTickAt;
        if (dt >= TICK_MS) {
            const elapsed = now - this.startedAt;
            const cycle = (elapsed % DURATION_MS) / DURATION_MS;
            const targetBytes = Math.floor(TOTAL * cycle);
            const jitter = 0.7 + Math.random() * 0.6;
            const instSpeed = Math.max(0, ((targetBytes - this.lastBytes) / dt) * 1000 * jitter);
            this.speedBps = this.speedBps === 0
                ? instSpeed
                : this.speedBps + EWMA_ALPHA * (instSpeed - this.speedBps);
            this.bytesDone = targetBytes;
            this.lastBytes = targetBytes;
            this.lastTickAt = now;
        }
        this.raf = requestAnimationFrame(this.loop);
    };

    render() {
        const sz = demoFormatSize(this.bytesDone, TOTAL);
        const sp = demoFormatSpeed(this.speedBps);
        return html`
            <dial-telemetry
                .sizeDoneText=${sz.doneText}
                .sizeTotalText=${sz.totalText}
                .sizeUnit=${sz.unit}
                .speedText=${sp.text}
                .speedUnit=${sp.unit}
                .bytesTotal=${TOTAL}
                .logicalMbps=${this.speedBps > 0 ? 1 : 0}
            ></dial-telemetry>
        `;
    }
}

interface Args {
    speedBps: number;
    bytesDone: number;
    bytesTotal: number;
}

export default {
    title: "Components / Dial Telemetry",
    component: "dial-telemetry",
    argTypes: {
        speedBps: { control: { type: "range", min: 0, max: 200 * 1024 * 1024, step: 1024 } },
        bytesDone: { control: { type: "range", min: 0, max: 1024 * 1024 * 1024, step: 1024 * 1024 } },
        bytesTotal: { control: { type: "range", min: 0, max: 1024 * 1024 * 1024, step: 1024 * 1024 } },
    },
    args: {
        speedBps: 42 * 1024 * 1024,
        bytesDone: 412 * 1024 * 1024,
        bytesTotal: 980 * 1024 * 1024,
    },
};

export const Playground = (a: Args) => {
    const sz = demoFormatSize(a.bytesDone, a.bytesTotal);
    const sp = demoFormatSpeed(a.speedBps);
    return html`
        <dial-telemetry
            .sizeDoneText=${sz.doneText}
            .sizeTotalText=${sz.totalText}
            .sizeUnit=${sz.unit}
            .speedText=${sp.text}
            .speedUnit=${sp.unit}
            .bytesTotal=${a.bytesTotal}
            .logicalMbps=${a.speedBps > 0 ? 1 : 0}
        ></dial-telemetry>
    `;
};

export const RunBusy = () => html`<dial-telemetry-run-busy></dial-telemetry-run-busy>`;

export const TotalUnknown = () => {
    const sz = demoFormatSize(42 * 1024 * 1024, 0);
    const sp = demoFormatSpeed(1 * 1024 * 1024);
    return html`
        <dial-telemetry
            .sizeDoneText=${sz.doneText}
            .sizeTotalText=${sz.totalText}
            .sizeUnit=${sz.unit}
            .speedText=${sp.text}
            .speedUnit=${sp.unit}
            .bytesTotal=${0}
            .logicalMbps=${1}
        ></dial-telemetry>
    `;
};
