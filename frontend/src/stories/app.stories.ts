import { LitElement, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import "../ritual-app";
import { JoinAddress, Phase, Stage, ViewModel } from "../wails-api";
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

const downloading = (progress: number, speedMbps = 32): ViewModel =>
    new ViewModel({
        stage: Stage.StageDownloading,
        phase: Phase.PhaseDownloading,
        progress,
        bytesDone: Math.floor(1_000_000_000 * (progress / 100)),
        bytesTotal: 1_000_000_000,
        speedMbps,
    });

const preparing = (): ViewModel =>
    new ViewModel({
        stage: Stage.StageDownloading,
        phase: Phase.PhasePreparing,
        bytesDone: 1_000_000_000,
        bytesTotal: 1_000_000_000,
    });

const saving = (progress: number, speedMbps = 22): ViewModel =>
    new ViewModel({
        stage: Stage.StageUploading,
        phase: Phase.PhaseSaving,
        progress,
        bytesDone: Math.floor(1_000_000_000 * (progress / 100)),
        bytesTotal: 1_000_000_000,
        speedMbps,
    });

const wrapping = (): ViewModel =>
    new ViewModel({ stage: Stage.StageUploading, phase: Phase.PhaseWrapping });

const savingTail = (): ViewModel =>
    new ViewModel({
        stage: Stage.StageUploading,
        phase: Phase.PhaseSaving,
        bytesDone: 1_000_000_000,
        bytesTotal: 1_000_000_000,
    });

const playing = (): ViewModel =>
    new ViewModel({ stage: Stage.StageRunning, phase: Phase.PhasePlaying, addresses: ADDRESSES });

const idle = (): ViewModel => new ViewModel({ stage: Stage.StageIdle, phase: Phase.PhaseIdle });

// Lock conflict — design-log/017 folds it into PhaseFailed with lockHolder
// populated so the dial renders friendly "{holder} is playing" copy.
const lockedFailed = (holder: string): ViewModel =>
    new ViewModel({
        stage: Stage.StageFailed,
        phase: Phase.PhaseFailed,
        lockHolder: holder,
        errorText: `already locked by ${holder}`,
    });

const failedAt = (priorPhase: Phase, prior: ViewModel): ViewModel =>
    new ViewModel({
        stage: Stage.StageFailed,
        phase: Phase.PhaseFailed,
        progress: prior.progress,
        bytesDone: prior.bytesDone,
        bytesTotal: prior.bytesTotal,
        speedMbps: 0,
        addresses: priorPhase === Phase.PhasePlaying ? ADDRESSES : [],
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

// Full eight-phase walk per design-log/017 §Examples ✅ After:
// idle → downloading (bytes flow) → preparing (brain-cog plateau) →
// playing (addresses) → wrapping (unplug plateau) → saving (bytes flow) →
// saving-tail (sub goes empty) → idle.
export const HappyPath = () => {
    const beats: Beat[] = [
        { vm: idle(), holdMs: 800 },
        ...ramp(downloading, 0, 100, 18, 140),
        { vm: preparing(), holdMs: 1600 },
        { vm: playing(), holdMs: 2400 },
        { vm: wrapping(), holdMs: 1400 },
        ...ramp(saving, 0, 100, 18, 140),
        { vm: savingTail(), holdMs: 1200 },
        { vm: idle(), holdMs: 2000 },
    ];
    return html`<app-driver .beats=${beats}></app-driver>`;
};

// Lock-conflict story: someone else holds the lock, surfaces as PhaseFailed
// with a friendly "{holder} is playing" title (no scary error copy).
export const LockedConflict = () => {
    const beats: Beat[] = [{ vm: lockedFailed("alice"), holdMs: 10_000 }];
    return html`<app-driver .beats=${beats}></app-driver>`;
};

export const FailedDuringDownload = () => {
    const prior = downloading(42);
    const beats: Beat[] = [
        { vm: idle(), holdMs: 500 },
        ...ramp(downloading, 0, 42, 8, 140),
        { vm: failedAt(Phase.PhaseDownloading, prior), holdMs: 10_000 },
    ];
    return html`<app-driver .beats=${beats}></app-driver>`;
};

export const FailedDuringPreparing = () => {
    const beats: Beat[] = [
        { vm: idle(), holdMs: 500 },
        ...ramp(downloading, 0, 100, 8, 140),
        { vm: preparing(), holdMs: 600 },
        { vm: failedAt(Phase.PhasePreparing, preparing()), holdMs: 10_000 },
    ];
    return html`<app-driver .beats=${beats}></app-driver>`;
};

export const FailedDuringPlaying = () => {
    const beats: Beat[] = [
        { vm: idle(), holdMs: 300 },
        ...ramp(downloading, 0, 100, 8, 140),
        { vm: preparing(), holdMs: 600 },
        { vm: playing(), holdMs: 1200 },
        { vm: failedAt(Phase.PhasePlaying, playing()), holdMs: 10_000 },
    ];
    return html`<app-driver .beats=${beats}></app-driver>`;
};

export const FailedDuringSaving = () => {
    const prior = saving(67);
    const beats: Beat[] = [
        { vm: idle(), holdMs: 300 },
        ...ramp(downloading, 0, 100, 6, 140),
        { vm: preparing(), holdMs: 400 },
        { vm: playing(), holdMs: 800 },
        { vm: wrapping(), holdMs: 600 },
        ...ramp(saving, 0, 67, 10, 140),
        { vm: failedAt(Phase.PhaseSaving, prior), holdMs: 10_000 },
    ];
    return html`<app-driver .beats=${beats}></app-driver>`;
};

declare global {
    interface HTMLElementTagNameMap {
        "app-driver": AppDriver;
    }
}
