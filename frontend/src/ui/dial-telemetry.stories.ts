import { LitElement, html } from "lit";
import { customElement, state } from "lit/decorators.js";
import "./dial-telemetry";

const TOTAL = 980 * 1024 * 1024;
const DURATION_MS = 8000;
const TICK_MS = 100;
const EWMA_ALPHA = 0.18;

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
        return html`
            <dial-telemetry
                .speedBps=${this.speedBps}
                .bytesDone=${this.bytesDone}
                .bytesTotal=${TOTAL}
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

export const Playground = (a: Args) => html`
    <dial-telemetry
        .speedBps=${a.speedBps}
        .bytesDone=${a.bytesDone}
        .bytesTotal=${a.bytesTotal}
    ></dial-telemetry>
`;

export const RunBusy = () => html`<dial-telemetry-run-busy></dial-telemetry-run-busy>`;

export const TotalUnknown = () => html`
    <dial-telemetry
        .speedBps=${1 * 1024 * 1024}
        .bytesDone=${42 * 1024 * 1024}
        .bytesTotal=${0}
    ></dial-telemetry>
`;
