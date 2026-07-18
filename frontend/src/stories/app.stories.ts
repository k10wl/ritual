import { LitElement, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import "../ritual-app";
import { JoinAddress, Phase, Stage, ViewModel } from "../wails-api";
import { pushView, setSyncStatus } from "../../.storybook/preview";

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

const downloading = (progress: number, logicalMbps = 32): ViewModel =>
    new ViewModel({
        stage: Stage.StageDownloading,
        phase: Phase.PhaseDownloading,
        progress,
        bytesDone: Math.floor(1_000_000_000 * (progress / 100)),
        bytesTotal: 1_000_000_000,
        logicalMbps,
    });

const preparing = (): ViewModel =>
    new ViewModel({
        stage: Stage.StageDownloading,
        phase: Phase.PhasePreparing,
        bytesDone: 1_000_000_000,
        bytesTotal: 1_000_000_000,
    });

const saving = (progress: number, logicalMbps = 22): ViewModel =>
    new ViewModel({
        stage: Stage.StageUploading,
        phase: Phase.PhaseSaving,
        progress,
        bytesDone: Math.floor(1_000_000_000 * (progress / 100)),
        bytesTotal: 1_000_000_000,
        logicalMbps,
    });

// A saving beat whose link has gone quiet mid-transfer: projection sets
// stalled (driven by the ticker heartbeat over a frozen counter), so the dial
// swaps its ETA sub for "Stalled — waiting on R2…". Design-log/022 #2.
const savingStalled = (progress: number): ViewModel =>
    new ViewModel({
        stage: Stage.StageUploading,
        phase: Phase.PhaseSaving,
        progress,
        bytesDone: Math.floor(1_000_000_000 * (progress / 100)),
        bytesTotal: 1_000_000_000,
        logicalMbps: 0,
        stalled: true,
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
        logicalMbps: 0,
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

// Interactive end-to-end story — every flow is reachable here with NO backend,
// driven by the mock transport in .storybook/preview.ts. The behind/dirty/
// unpushed controls feed setSyncStatus() so the sync verdict is live-tunable.
//
// What to click to reach each flow:
//   • dial → normal session (download → spin-up → live); hold the dial (Stop)
//     → spin-down → save → idle.
//   • Advanced → Settings → tick "Skip sync this session" → back → dial →
//     local-only launch (no download ramp, straight to spin-up → live).
//   • Advanced → Sync → Download / Publish. With `behind` on, Publish shows the
//     loud "remote is newer" warning.
//   • Flip `dirty` or `unpushed` → IDLE "Unpublished changes" cue appears under
//     Advanced, and Sync offers Publish.
//   • Fail mid-sync (see FailedDuring* stories) → "Skip sync & run locally" hint.
interface LiveArgs {
    behind: boolean;
    dirty: boolean;
    unpushed: boolean;
}

const HEAD_OLD = "2026-05-30T08-00-00.000Z";
const HEAD_NEW = "2026-05-30T09-00-00.000Z";

export const Live = {
    args: { behind: false, dirty: false, unpushed: false },
    argTypes: {
        behind: { control: "boolean" },
        dirty: { control: "boolean" },
        unpushed: { control: "boolean" },
    },
    render: ({ behind, dirty, unpushed }: LiveArgs) => {
        // behind ⇒ remoteHead newer than localHead; unpushed ⇒ localHead newer
        // than remoteHead. They can't both order the heads, so behind wins the
        // ordering while unpushed is still reported as the arg (design-log/035).
        let localHead = "";
        let remoteHead = "";
        if (behind) {
            localHead = HEAD_OLD;
            remoteHead = HEAD_NEW;
        } else if (unpushed) {
            localHead = HEAD_NEW;
            remoteHead = HEAD_OLD;
        }
        setSyncStatus({ behind, dirty, unpushed, localHead, remoteHead });
        return html`<ritual-app></ritual-app>`;
    },
};

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

// Wire-stall story: an upload stalls mid-transfer (a quiet R2 PutStream). The
// dial holds its arc and "Saving" label while the sub reads "Stalled — waiting
// on R2…", then clears the moment bytes resume. Design-log/022 #2.
export const StalledUpload = () => {
    const beats: Beat[] = [
        ...ramp(saving, 0, 47, 8, 140),
        { vm: savingStalled(47), holdMs: 4000 },
        ...ramp(saving, 47, 100, 10, 140),
        { vm: savingTail(), holdMs: 1200 },
        { vm: idle(), holdMs: 1500 },
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

// Skip-sync local-only session (design-log/036): for testing — not pulling,
// not pushing, and saving nothing. NO download ramp and NO save tail: the
// launch goes idle → spin-up → live → spin-down → idle. Mirrors the mock
// transport's skipSync=true Start path (BuildLocalSession: Checking → Running
// → Done).
export const SkipSyncLaunch = () => {
    const beats: Beat[] = [
        { vm: idle(), holdMs: 800 },
        { vm: preparing(), holdMs: 1600 },
        { vm: playing(), holdMs: 2400 },
        { vm: wrapping(), holdMs: 1400 },
        { vm: idle(), holdMs: 2000 },
    ];
    return html`<app-driver .beats=${beats}></app-driver>`;
};

declare global {
    interface HTMLElementTagNameMap {
        "app-driver": AppDriver;
    }
}
