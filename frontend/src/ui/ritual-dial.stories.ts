import { LitElement, html } from "lit";
import { customElement, state } from "lit/decorators.js";
import { gsap } from "gsap";
import "./ritual-dial";
import type { DialGlyph, DialState } from "./ritual-dial";

const TOTAL_MB = 953;
const TRANSFER_S = 3;
const PHASE_S = 1.4;
const PLATEAU_S = 1.6;

// Full eight-phase cycle per design-log/017 §Examples ✅ After.
// idle → downloading (bytes 0→1) → preparing (brain-cog plateau at 1) →
// playing (stop glyph plateau) → wrapping (unplug plateau at 0) →
// saving (bytes 0→1) → saving-tail (sub empties) → fail → idle.
@customElement("dial-cycle-demo")
export class DialCycleDemo extends LitElement {
    @state() private state: DialState = "idle";
    @state() private arc = 0;
    @state() private glyph: DialGlyph = "play";
    @state() private label = "Start";
    @state() private sub = "";

    private tl?: gsap.core.Timeline;

    connectedCallback() {
        super.connectedCallback();
        const a = { v: 0 };
        const writeProgress = () => {
            this.arc = a.v;
            this.sub = `${Math.round(a.v * TOTAL_MB)} of ${TOTAL_MB} MB`;
        };
        const tl = gsap.timeline({ repeat: -1 });

        // Idle
        tl.call(() => {
            this.state = "idle"; this.arc = 0; this.glyph = "play";
            this.label = "Start"; this.sub = "";
        });
        tl.to({}, { duration: PLATEAU_S });

        // Downloading (bytes flow, ETA-style sub)
        tl.call(() => {
            this.state = "prep"; this.glyph = "download"; this.label = "Downloading";
            a.v = 0; writeProgress();
        });
        tl.to(a, { v: 1, duration: TRANSFER_S, ease: "power2.out", onUpdate: writeProgress });

        // Preparing (apply + acquire + boot; arc plateau at 1)
        tl.call(() => {
            this.state = "prep"; this.glyph = "brain-cog"; this.label = "Spinning up";
            this.arc = 1; this.sub = "Almost live";
        });
        tl.to({}, { duration: PHASE_S });

        // Playing (server ready, glow)
        tl.call(() => {
            this.state = "run"; this.arc = 1; this.glyph = "stop";
            this.label = "Live"; this.sub = "0:00:42";
        });
        tl.to({}, { duration: PLATEAU_S });

        // Wrapping (ServerStopping + Committing; arc plateau at 0)
        tl.call(() => {
            this.state = "final"; this.glyph = "unplug"; this.label = "Spinning down";
            this.arc = 0; this.sub = "Going offline";
        });
        tl.to({}, { duration: PHASE_S });

        // Saving (Pushing bytes flow, ETA-style sub)
        tl.call(() => {
            this.state = "final"; this.glyph = "upload"; this.label = "Saving";
            a.v = 0; writeProgress();
        });
        tl.to(a, { v: 1, duration: TRANSFER_S, ease: "power2.out", onUpdate: writeProgress });

        // Save-tail (Unlocking + Retaining; title swap + brief sub)
        tl.call(() => {
            this.state = "final"; this.glyph = "upload"; this.label = "Wrapping up";
            this.arc = 1; this.sub = "Almost done";
        });
        tl.to({}, { duration: PHASE_S });

        // Failure overlay (dismiss-to-idle copy)
        tl.call(() => {
            this.state = "fail"; this.arc = 0.42; this.glyph = "x";
            this.label = "Couldn't finish saving"; this.sub = "Tap to dismiss";
        });
        tl.to({}, { duration: PLATEAU_S });
        this.tl = tl;
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        this.tl?.kill();
    }

    render() {
        return html`
            <ritual-dial
                .state=${this.state}
                .arc=${this.arc}
                .glyph=${this.glyph}
                .label=${this.label}
                .sub=${this.sub}
            ></ritual-dial>
        `;
    }
}

const TOTAL_FILES = 240;

// Workroot relocate cycle (design-log/055 addendum): idle → RelocateStarted
// (moving files, 0%) → RelocatePlanned/RelocateProgress (files flow 0→total,
// file-count sub — relocate has no byte-rate counter, unlike the session's
// download/save beats above) → RelocateVerifying/RelocateCommitting
// (plateau, "Finishing up") → RelocateFinished (idle) → failure overlay →
// idle. Mirrors DialCycleDemo's shape one-for-one against relocate's actual
// event sequence (internal/core/stages/relocating/strategy.go).
@customElement("relocate-cycle-demo")
export class RelocateCycleDemo extends LitElement {
    @state() private state: DialState = "idle";
    @state() private arc = 0;
    @state() private glyph: DialGlyph = "play";
    @state() private label = "Start";
    @state() private sub = "";

    private tl?: gsap.core.Timeline;

    connectedCallback() {
        super.connectedCallback();
        const f = { v: 0 };
        const writeProgress = () => {
            this.arc = f.v;
            const done = Math.round(f.v * TOTAL_FILES);
            this.sub = `${done} of ${TOTAL_FILES} files`;
        };
        const tl = gsap.timeline({ repeat: -1 });

        // Idle
        tl.call(() => {
            this.state = "idle"; this.arc = 0; this.glyph = "play";
            this.label = "Start"; this.sub = "";
        });
        tl.to({}, { duration: PLATEAU_S });

        // RelocateStarted + RelocatePlanned: dial flips, sub appears at 0/total
        tl.call(() => {
            this.state = "prep"; this.glyph = "folder-input"; this.label = "Moving files";
            f.v = 0; writeProgress();
        });
        tl.to({}, { duration: PHASE_S * 0.5 });

        // RelocateProgress: files flow (file-count, not byte-rate)
        tl.to(f, { v: 1, duration: TRANSFER_S, ease: "power2.out", onUpdate: writeProgress });

        // RelocateVerifying + RelocateCommitting: fixed-cost tail, plateau
        tl.call(() => {
            this.arc = 1; this.sub = "Finishing up";
        });
        tl.to({}, { duration: PHASE_S });

        // RelocateFinished
        tl.call(() => {
            this.state = "idle"; this.arc = 0; this.glyph = "play";
            this.label = "Start"; this.sub = "";
        });
        tl.to({}, { duration: PLATEAU_S });

        // RelocateFailed overlay (dismiss-to-idle copy)
        tl.call(() => {
            this.state = "fail"; this.arc = 0.6; this.glyph = "x";
            this.label = "Couldn't move files"; this.sub = "Tap to dismiss";
        });
        tl.to({}, { duration: PLATEAU_S });
        this.tl = tl;
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        this.tl?.kill();
    }

    render() {
        return html`
            <ritual-dial
                .state=${this.state}
                .arc=${this.arc}
                .glyph=${this.glyph}
                .label=${this.label}
                .sub=${this.sub}
            ></ritual-dial>
        `;
    }
}

interface Args {
    state: DialState;
    arc: number;
    label: string;
    sub: string;
    glyph: DialGlyph | "";
    disabled: boolean;
}

export default {
    title: "Components / Ritual Dial",
    component: "ritual-dial",
    argTypes: {
        state: {
            control: { type: "select" },
            options: ["idle", "prep", "run", "final", "fail", "preflight"],
        },
        arc: { control: { type: "range", min: 0, max: 1, step: 0.01 } },
        label: { control: { type: "text" } },
        sub: { control: { type: "text" } },
        glyph: {
            control: { type: "select" },
            options: ["", "play", "stop", "x", "download", "upload", "brain-cog", "unplug", "folder-input"],
        },
        disabled: { control: { type: "boolean" } },
    },
    args: {
        state: "idle",
        arc: 0,
        label: "Start",
        sub: "",
        glyph: "play",
        disabled: false,
    },
};

export const Playground = (a: Args) => html`
    <ritual-dial
        .state=${a.state}
        .arc=${a.arc}
        .label=${a.label}
        .sub=${a.sub}
        .glyph=${a.glyph === "" ? null : (a.glyph as DialGlyph)}
        ?disabled=${a.disabled}
    ></ritual-dial>
`;

// Beat snapshots — one per phase per design-log/017 §Visual dispatch.
export const PhaseDownloading = () => html`
    <ritual-dial state="prep" .arc=${0.42} glyph="download"
        label="Downloading" sub="about 1 minute"></ritual-dial>
`;

export const PhasePreparing = () => html`
    <ritual-dial state="prep" .arc=${1} glyph="brain-cog"
        label="Spinning up" sub="Almost live"></ritual-dial>
`;

export const PhasePlaying = () => html`
    <ritual-dial state="run" .arc=${1} glyph="stop"
        label="Live" sub="0:00:42"></ritual-dial>
`;

export const PhaseWrapping = () => html`
    <ritual-dial state="final" .arc=${0} glyph="unplug"
        label="Spinning down" sub="Going offline"></ritual-dial>
`;

export const PhaseSaving = () => html`
    <ritual-dial state="final" .arc=${0.42} glyph="upload"
        label="Saving" sub="about 30 seconds"></ritual-dial>
`;

export const PhaseSavingTail = () => html`
    <ritual-dial state="final" .arc=${1} glyph="upload"
        label="Wrapping up" sub="Almost done"></ritual-dial>
`;

// Workroot relocate (design-log/055 addendum): files copying, file-count
// sub-line (relocateSub) rather than etaSub's byte-rate ETA — relocate has
// no speed counter (copyContent tracks files copied, not a byte stream).
export const PhaseRelocating = () => html`
    <ritual-dial state="prep" .arc=${0.42} glyph="folder-input"
        label="Moving files" sub="42 of 100 files"></ritual-dial>
`;

// Copying done, fixed-cost verify/commit tail — same arc-plateau +
// "Finishing up" pattern PhaseSavingTail uses for the equivalent moment in
// the save beat.
export const PhaseRelocatingTail = () => html`
    <ritual-dial state="prep" .arc=${1} glyph="folder-input"
        label="Moving files" sub="Finishing up"></ritual-dial>
`;

export const PhaseFailed = () => html`
    <ritual-dial state="fail" .arc=${0.42} glyph="x"
        label="Couldn't finish saving" sub="Tap to dismiss"></ritual-dial>
`;

// Lock conflict — folded into PhaseFailed per design-log/017.
export const PhaseFailedLocked = () => html`
    <ritual-dial state="fail" .arc=${0} glyph="x"
        label="alice is playing" sub="Tap to dismiss"></ritual-dial>
`;

// Autoupdate Preflight beats (design-log/037): gray, inert, no glyph.
export const PreflightChecking = () => html`
    <ritual-dial state="preflight" .arc=${1} .glyph=${null}
        label="" sub="Checking for updates"></ritual-dial>
`;

export const PreflightUpdating = () => html`
    <ritual-dial state="preflight" .arc=${1} .glyph=${null}
        label="" sub="Updating → v2.1.0"></ritual-dial>
`;

export const FailedUpdate = () => html`
    <ritual-dial state="fail" .arc=${0} glyph="x"
        label="" sub="Couldn't update — restart, or try Advanced ▸ Check for update"></ritual-dial>
`;

export const Cycle = () => html`<dial-cycle-demo></dial-cycle-demo>`;

// Workroot relocate, animated end-to-end (design-log/055 addendum): plays
// RelocateStarted → RelocatePlanned/RelocateProgress → RelocateVerifying/
// RelocateCommitting → RelocateFinished → RelocateFailed → idle, on repeat.
export const RelocateCycle = () => html`<relocate-cycle-demo></relocate-cycle-demo>`;
