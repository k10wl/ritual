import { LitElement, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import "../ritual-app";
import { JoinAddress, Stage, ViewModel } from "../wails-api";
import { pushView } from "../../.storybook/preview";

export default {
    title: "Screens / Ritual",
    component: "ritual-app",
    parameters: { frame: "bare" },
};

interface Beat {
    vm: ViewModel;
    holdMs: number;
}

const ADDRESSES = [
    new JoinAddress({ label: "LAN", address: "192.168.1.10:25565" }),
    new JoinAddress({ label: "Tailscale", address: "100.64.0.5:25565" }),
];

const prep = (progress: number, speedMbps = 32): ViewModel =>
    new ViewModel({
        stage: Stage.StageDownloading,
        progress,
        bytesDone: Math.floor(1_000_000_000 * (progress / 100)),
        bytesTotal: 1_000_000_000,
        speedMbps,
    });

const upload = (progress: number, speedMbps = 22): ViewModel =>
    new ViewModel({
        stage: Stage.StageUploading,
        progress,
        bytesDone: Math.floor(1_000_000_000 * (progress / 100)),
        bytesTotal: 1_000_000_000,
        speedMbps,
    });

const run = (): ViewModel =>
    new ViewModel({ stage: Stage.StageRunning, readyLight: true, addresses: ADDRESSES });

const idle = (): ViewModel => new ViewModel({ stage: Stage.StageIdle });
const locked = (holder: string): ViewModel =>
    new ViewModel({ stage: Stage.StageLocked, lockHolder: holder });
const failedAt = (priorStage: Stage, prior: ViewModel): ViewModel =>
    new ViewModel({
        stage: Stage.StageFailed,
        progress: prior.progress,
        bytesDone: prior.bytesDone,
        bytesTotal: prior.bytesTotal,
        speedMbps: 0,
        addresses: priorStage === Stage.StageRunning ? ADDRESSES : [],
        errorText: "transport reset",
    });

@customElement("app-driver")
export class AppDriver extends LitElement {
    @property({ attribute: false }) beats: Beat[] = [];
    private timer = 0;
    private idx = 0;

    connectedCallback() {
        super.connectedCallback();
        this.play();
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        this.stop();
    }

    private play() {
        this.stop();
        this.idx = 0;
        const tick = () => {
            if (this.idx >= this.beats.length) return;
            const beat = this.beats[this.idx++];
            pushView(beat.vm);
            this.timer = window.setTimeout(tick, beat.holdMs);
        };
        tick();
    }

    private stop() {
        if (!this.timer) return;
        clearTimeout(this.timer);
        this.timer = 0;
    }

    render() {
        return html`<ritual-app></ritual-app>`;
    }
}

const ramp = (
    make: (p: number) => ViewModel,
    fromPct: number,
    toPct: number,
    steps: number,
    perBeatMs: number,
): Beat[] => {
    const out: Beat[] = [];
    for (let i = 0; i <= steps; i++) {
        const p = fromPct + (toPct - fromPct) * (i / steps);
        out.push({ vm: make(p), holdMs: perBeatMs });
    }
    return out;
};

export const Live = () => html`<ritual-app></ritual-app>`;

export const HappyPath = () => {
    const beats: Beat[] = [
        { vm: idle(), holdMs: 800 },
        ...ramp(prep, 0, 100, 18, 140),
        { vm: run(), holdMs: 2400 },
        ...ramp(upload, 0, 100, 18, 140),
        { vm: idle(), holdMs: 2000 },
    ];
    return html`<app-driver .beats=${beats}></app-driver>`;
};

export const LockedIdle = () => {
    const beats: Beat[] = [{ vm: locked("alice"), holdMs: 10_000 }];
    return html`<app-driver .beats=${beats}></app-driver>`;
};

export const FailedDuringPrep = () => {
    const prior = prep(42);
    const beats: Beat[] = [
        { vm: idle(), holdMs: 500 },
        ...ramp(prep, 0, 42, 8, 140),
        { vm: failedAt(Stage.StageDownloading, prior), holdMs: 10_000 },
    ];
    return html`<app-driver .beats=${beats}></app-driver>`;
};

export const FailedDuringRun = () => {
    const beats: Beat[] = [
        { vm: idle(), holdMs: 300 },
        ...ramp(prep, 0, 100, 8, 140),
        { vm: run(), holdMs: 1200 },
        { vm: failedAt(Stage.StageRunning, run()), holdMs: 10_000 },
    ];
    return html`<app-driver .beats=${beats}></app-driver>`;
};

export const FailedDuringFinal = () => {
    const prior = upload(67);
    const beats: Beat[] = [
        { vm: idle(), holdMs: 300 },
        ...ramp(prep, 0, 100, 6, 140),
        { vm: run(), holdMs: 800 },
        ...ramp(upload, 0, 67, 10, 140),
        { vm: failedAt(Stage.StageUploading, prior), holdMs: 10_000 },
    ];
    return html`<app-driver .beats=${beats}></app-driver>`;
};

declare global {
    interface HTMLElementTagNameMap {
        "app-driver": AppDriver;
    }
}
