import { css, html, LitElement } from "lit";
import { customElement, query, state } from "lit/decorators.js";
import {
    getPrep,
    getSnapshot,
    onView,
    openRootFolder,
    showLogs,
    start,
    stop,
    Stage,
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

interface Derived {
    dial: { state: DialState; arc: number; glyph: DialGlyph; label: string; sub: string };
    underSlot: UnderSlot;
    telemetry: { speedBps: number; bytesDone: number; bytesTotal: number };
    addresses: JoinAddress[];
}

const FALLBACK_VM: ViewModel = new ViewModel({ stage: Stage.StageIdle });

function nounFor(stage: Stage): string {
    switch (stage) {
        case Stage.StageDownloading: return "getting ready";
        case Stage.StageRunning:     return "running the server";
        case Stage.StageUploading:   return "saving";
        default:                     return "the run";
    }
}

function snapEta(secs: number): number {
    if (secs < 60) return Math.round(secs);
    if (secs < 600) return Math.round(secs / 10) * 10;
    return Math.round(secs / 60) * 60;
}

@customElement("ritual-app")
export class RitualApp extends LitElement {
    @state() private vm: ViewModel = FALLBACK_VM;
    @state() private lastProgressArc = 0;
    @state() private lastNonFailStage: Stage = Stage.StageIdle;
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
        const wasRun = this.vm.stage === Stage.StageRunning;
        const isRun = vm.stage === Stage.StageRunning;
        if (vm.stage !== Stage.StageFailed) {
            this.lastNonFailStage = vm.stage;
            this.lastProgressArc = this.arcFor(vm);
        }
        if (isRun && !wasRun) this.startUptime();
        if (!isRun && wasRun) this.stopUptime();
        this.vm = vm;
    }

    private arcFor(vm: ViewModel): number {
        if (vm.bytesTotal <= 0) return 0;
        return Math.min(1, Math.max(0, vm.bytesDone / vm.bytesTotal));
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

    private etaSub(vm: ViewModel): string {
        const speedBps = vm.speedMbps * MBPS_TO_BPS;
        if (speedBps <= 0 || vm.bytesTotal <= 0) return formatEta(null);
        const remaining = Math.max(0, vm.bytesTotal - vm.bytesDone);
        return formatEta(snapEta(remaining / speedBps));
    }

    private derive(): Derived {
        const vm = this.vm;
        const telemetry = {
            speedBps: vm.speedMbps * MBPS_TO_BPS,
            bytesDone: vm.bytesDone,
            bytesTotal: vm.bytesTotal,
        };
        if (vm.stage === Stage.StageFailed) {
            return {
                dial: {
                    state: "fail",
                    arc: this.lastProgressArc,
                    glyph: "x",
                    label: `Couldn't finish ${nounFor(this.lastNonFailStage)}`,
                    sub: "Tap to try again",
                },
                underSlot: null,
                telemetry,
                addresses: vm.addresses,
            };
        }
        switch (vm.stage) {
            case Stage.StageLocked:
                return {
                    dial: {
                        state: "idle",
                        arc: 0,
                        glyph: "x",
                        label: vm.lockHolder ? `${vm.lockHolder} is playing` : "Locked",
                        sub: "Tap to check again",
                    },
                    underSlot: null,
                    telemetry,
                    addresses: vm.addresses,
                };
            case Stage.StageDownloading:
                return {
                    dial: {
                        state: "prep",
                        arc: this.arcFor(vm),
                        glyph: "download",
                        label: "Getting ready",
                        sub: this.etaSub(vm),
                    },
                    underSlot: "telemetry",
                    telemetry,
                    addresses: vm.addresses,
                };
            case Stage.StageRunning:
                return {
                    dial: {
                        state: "run",
                        arc: 1,
                        glyph: "stop",
                        label: "Ready to play",
                        sub: this.uptimeSub || formatEta(0),
                    },
                    underSlot: "addresses",
                    telemetry,
                    addresses: vm.addresses,
                };
            case Stage.StageUploading:
                return {
                    dial: {
                        state: "final",
                        arc: this.arcFor(vm),
                        glyph: "upload",
                        label: "Saving",
                        sub: this.etaSub(vm),
                    },
                    underSlot: "telemetry",
                    telemetry,
                    addresses: vm.addresses,
                };
            case Stage.StageIdle:
            default:
                return {
                    dial: { state: "idle", arc: 0, glyph: "play", label: "Start", sub: "" },
                    underSlot: null,
                    telemetry,
                    addresses: vm.addresses,
                };
        }
    }

    private onTap = () => {
        const s = this.vm.stage;
        if (s !== Stage.StageIdle && s !== Stage.StageLocked && s !== Stage.StageFailed) return;
        const settings = this._prepEl?.read() ?? this.prep;
        void start(settings.port, settings.memoryMB);
    };

    private onHoldCommit = () => {
        if (this.vm.stage === Stage.StageRunning) void stop();
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
