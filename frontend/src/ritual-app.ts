import { css, html, LitElement } from "lit";
import { customElement, query, state } from "lit/decorators.js";
import {
    dismiss,
    getPrep,
    getSnapshot,
    onView,
    openRootFolder,
    showLogs,
    start,
    stop,
    Phase,
    ViewModel,
    JoinAddress,
} from "./wails-api";
import type { DialGlyph, DialState } from "./ui/ritual-dial";
import { formatEta } from "./ui/telemetry-format";
import "./ui/ritual-shell";
import "./ui/ritual-dial";
import "./ui/dial-telemetry";
import "./ui/run-addresses";
import "./ui/prep-settings";
import type { PrepSettings, PrepSettingsEl } from "./ui/prep-settings";

const MBPS_TO_BPS = 1_000_000 / 8;
const FALLBACK_PREP: PrepSettings = { port: 25565, memoryMB: 4096 };

type UnderSlot = "telemetry" | "addresses" | null;

interface DialView {
    state: DialState;
    glyph: DialGlyph;
    label: string;
    arc: (vm: ViewModel, ctx: AppCtx) => number;
    sub: (vm: ViewModel, ctx: AppCtx) => string;
    underSlot: UnderSlot;
}

interface AppCtx {
    uptimeSub: string;
    lastProgressArc: number;
    lastNonFailPhase: Phase;
}

const FALLBACK_VM: ViewModel = new ViewModel({ phase: Phase.PhaseIdle });

// Failure attribution noun map: when a run fails, the dial shows "Couldn't
// finish {noun}" where noun reflects the last user-meaningful phase the run
// was in. Three nouns cover the six active phases. Per design-log/017.
function nounFor(phase: Phase): string {
    switch (phase) {
        case Phase.PhaseDownloading: return "starting";
        case Phase.PhasePreparing:   return "starting";
        case Phase.PhasePlaying:     return "running";
        case Phase.PhaseWrapping:    return "saving";
        case Phase.PhaseSaving:      return "saving";
        default:                     return "the run";
    }
}

function snapEta(secs: number): number {
    if (secs < 60) return Math.round(secs);
    if (secs < 600) return Math.round(secs / 10) * 10;
    return Math.round(secs / 60) * 60;
}

function arcFromBytes(vm: ViewModel): number {
    if (vm.bytesTotal <= 0) return 0;
    return Math.min(1, Math.max(0, vm.bytesDone / vm.bytesTotal));
}

// Logical rate, not wire: bytesTotal/bytesDone are logical (Stream.Data /
// PlanInfo). Pairing them with vm.speedMbps (wire) over-shoots ETA by the
// compression factor on compressible payloads. SpeedMbps stays on the
// ViewModel for logs + future dual-series chart. See design-log/018.
function etaSub(vm: ViewModel): string {
    const speedBps = vm.logicalMbps * MBPS_TO_BPS;
    if (speedBps <= 0 || vm.bytesTotal <= 0) return formatEta(null);
    const remaining = Math.max(0, vm.bytesTotal - vm.bytesDone);
    return formatEta(snapEta(remaining / speedBps));
}

// Phase → dial view table. Single source of truth for glyph + label + arc +
// sub-line + under-slot dispatch. Per design-log/017 §Visual dispatch +
// copy table. Lock-conflict is a PhaseFailed beat with friendly copy
// resolved at render time (sees vm.lockHolder).
const PHASE_VIEW: Record<Phase, DialView> = {
    [Phase.$zero]: {
        state: "idle", glyph: "play", label: "Start", underSlot: null,
        arc: () => 0,
        sub: () => "",
    },
    [Phase.PhaseIdle]: {
        state: "idle", glyph: "play", label: "Start", underSlot: null,
        arc: () => 0,
        sub: () => "",
    },
    [Phase.PhaseDownloading]: {
        state: "prep", glyph: "download", label: "Downloading", underSlot: "telemetry",
        arc: arcFromBytes,
        sub: etaSub,
    },
    [Phase.PhasePreparing]: {
        state: "prep", glyph: "brain-cog", label: "Spinning up", underSlot: null,
        arc: () => 1,
        sub: () => "Almost live",
    },
    [Phase.PhasePlaying]: {
        state: "run", glyph: "stop", label: "Live", underSlot: "addresses",
        arc: () => 1,
        sub: (_vm, ctx) => ctx.uptimeSub || formatEta(0),
    },
    [Phase.PhaseWrapping]: {
        state: "final", glyph: "unplug", label: "Spinning down", underSlot: null,
        arc: () => 0,
        sub: () => "Going offline",
    },
    [Phase.PhaseSaving]: {
        state: "final", glyph: "upload", label: "Saving", underSlot: "telemetry",
        arc: arcFromBytes,
        // Save-tail per design-log/017: once all bytes are out (Unlocking +
        // Retaining), swap the visible label to "Wrapping up" / "Almost done"
        // — the arc plateau + housekeeping deserves its own beat copy.
        sub: (vm) => (vm.bytesTotal > 0 && vm.bytesDone >= vm.bytesTotal ? "Almost done" : etaSub(vm)),
    },
    [Phase.PhaseFailed]: {
        state: "fail", glyph: "x", label: "",
        // Failure label resolved at render time so locked-conflict and
        // generic failures pick distinct copy from the same dispatch slot.
        underSlot: null,
        arc: (_vm, ctx) => ctx.lastProgressArc,
        sub: () => "Tap to dismiss",
    },
};

interface Derived {
    dial: { state: DialState; arc: number; glyph: DialGlyph; label: string; sub: string };
    underSlot: UnderSlot;
    telemetry: { speedBps: number; bytesDone: number; bytesTotal: number };
    addresses: JoinAddress[];
}

@customElement("ritual-app")
export class RitualApp extends LitElement {
    @state() private vm: ViewModel = FALLBACK_VM;
    @state() private lastProgressArc = 0;
    @state() private lastNonFailPhase: Phase = Phase.PhaseIdle;
    @state() private uptimeSub = "";
    @state() private prep: PrepSettings = FALLBACK_PREP;
    @query("prep-settings") private _prepEl!: PrepSettingsEl | null;
    private runStartedAt = 0;
    private uptimeTimer = 0;
    private unsubscribe?: () => void;

    async connectedCallback() {
        super.connectedCallback();
        try {
            this.applyVm(await getSnapshot());
        } catch {
            // first render relies on FALLBACK_VM until the first Emit arrives
        }
        try {
            const p = await getPrep();
            this.prep = { port: p.port, memoryMB: p.memoryMB };
        } catch {
            // keep FALLBACK_PREP if the binding is unavailable
        }
        this.unsubscribe = onView((vm) => this.applyVm(vm));
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        this.unsubscribe?.();
        this.stopUptime();
    }

    private applyVm(vm: ViewModel) {
        const wasPlaying = this.vm.phase === Phase.PhasePlaying;
        const isPlaying = vm.phase === Phase.PhasePlaying;
        if (vm.phase !== Phase.PhaseFailed) {
            this.lastNonFailPhase = vm.phase;
            this.lastProgressArc = PHASE_VIEW[vm.phase]?.arc(vm, this.ctx()) ?? 0;
        }
        if (isPlaying && !wasPlaying) this.startUptime();
        if (!isPlaying && wasPlaying) this.stopUptime();
        this.vm = vm;
    }

    private ctx(): AppCtx {
        return {
            uptimeSub: this.uptimeSub,
            lastProgressArc: this.lastProgressArc,
            lastNonFailPhase: this.lastNonFailPhase,
        };
    }

    private startUptime() {
        this.runStartedAt = performance.now();
        this.uptimeSub = formatEta(0);
        this.uptimeTimer = window.setInterval(() => {
            const elapsed = Math.floor((performance.now() - this.runStartedAt) / 1000);
            this.uptimeSub = formatEta(elapsed);
        }, 1000);
    }

    private stopUptime() {
        if (!this.uptimeTimer) return;
        clearInterval(this.uptimeTimer);
        this.uptimeTimer = 0;
    }

    private derive(): Derived {
        const vm = this.vm;
        const ctx = this.ctx();
        const telemetry = {
            speedBps: vm.logicalMbps * MBPS_TO_BPS,
            bytesDone: vm.bytesDone,
            bytesTotal: vm.bytesTotal,
        };
        const view = PHASE_VIEW[vm.phase] ?? PHASE_VIEW[Phase.PhaseIdle];

        let label = view.label;
        // Save-tail beat shares the dispatch slot with the saving beat; swap
        // its title to "Wrapping up" once the bytes window closes.
        if (vm.phase === Phase.PhaseSaving) {
            if (vm.bytesTotal > 0 && vm.bytesDone >= vm.bytesTotal) {
                label = "Wrapping up";
            } else {
                label = "Saving";
            }
        } else if (vm.phase === Phase.PhaseFailed) {
            // Lock-held failures get the friendly holder-name title; generic
            // failures pick a phase-attributed noun. Design-log/017 folded
            // PhaseLocked into PhaseFailed.
            label = vm.lockHolder
                ? `${vm.lockHolder} is playing`
                : `Couldn't finish ${nounFor(this.lastNonFailPhase)}`;
        }

        return {
            dial: {
                state: view.state,
                arc: view.arc(vm, ctx),
                glyph: view.glyph,
                label,
                sub: view.sub(vm, ctx),
            },
            underSlot: view.underSlot,
            telemetry,
            addresses: vm.addresses,
        };
    }

    private onTap = () => {
        const phase = this.vm.phase;
        if (phase === Phase.PhaseFailed) {
            void dismiss();
            return;
        }
        if (phase !== Phase.PhaseIdle && phase !== Phase.$zero) return;
        const settings = this._prepEl?.read() ?? this.prep;
        void start(settings.port, settings.memoryMB);
    };

    private onHoldCommit = () => {
        if (this.vm.phase === Phase.PhasePlaying) void stop();
    };

    private onAmbientAction = (e: CustomEvent<"logs" | "folder">) => {
        if (e.detail === "logs") void showLogs();
        else void openRootFolder();
    };

    private underSlotChild(d: Derived) {
        if (d.underSlot === "addresses") {
            return html`<run-addresses .addresses=${d.addresses}></run-addresses>`;
        }
        if (d.underSlot === "telemetry") {
            return html`<dial-telemetry
                .speedBps=${d.telemetry.speedBps}
                .bytesDone=${d.telemetry.bytesDone}
                .bytesTotal=${d.telemetry.bytesTotal}
            ></dial-telemetry>`;
        }
        return null;
    }

    render() {
        const d = this.derive();
        return html`
            <ritual-shell .state=${d.dial.state} @ambient-action=${this.onAmbientAction}>
                <ritual-dial
                    .state=${d.dial.state}
                    .arc=${d.dial.arc}
                    .glyph=${d.dial.glyph}
                    .label=${d.dial.label}
                    .sub=${d.dial.sub}
                    @tap=${this.onTap}
                    @hold-commit=${this.onHoldCommit}
                ></ritual-dial>
                <div class="under-slot" ?data-shown=${d.underSlot !== null}>
                    ${this.underSlotChild(d)}
                </div>
                ${d.dial.state === "idle"
                    ? html`<prep-settings .config=${this.prep}></prep-settings>`
                    : null}
            </ritual-shell>
        `;
    }

    static styles = css`
        :host { display: contents; }
        .under-slot {
            opacity: 0;
            transform: translateY(-4px);
            transition: opacity 240ms ease, transform 240ms ease;
            min-height: 1.5rem;
            width: 100%;
            display: flex;
            justify-content: center;
        }
        .under-slot[data-shown] {
            opacity: 1;
            transform: translateY(0);
        }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        "ritual-app": RitualApp;
    }
}
