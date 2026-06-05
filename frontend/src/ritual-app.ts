import { css, html, LitElement } from "lit";
import { customElement, query, state } from "lit/decorators.js";
import {
    applyRetentionNow,
    deleteLocalVersion,
    dismiss,
    checkForUpdate,
    download,
    getLocalStorageStats,
    getPrep,
    getRetentionRules,
    getSnapshot,
    getSyncStatus,
    listVersions,
    onView,
    restore,
    revert,
    setRetentionRules,
    showLogs,
    start,
    stop,
    upload,
    Phase,
    RetentionRules,
    ViewModel,
    JoinAddress,
} from "./wails-api";
import "./ui/primitives/decoder";
import type { DialGlyph, DialState } from "./ui/ritual-dial";
import { formatEta } from "./ui/telemetry-format";
import "./ui/ritual-shell";
import "./ui/ritual-dial";
import "./ui/dial-telemetry";
import "./ui/run-addresses";
import "./ui/run-console-link";
import "./ui/primitives/rune-stack";
import "./ui/advanced-view";
import type { RuneStack } from "./ui/primitives/rune-stack";
import type { NavView } from "./ui/contexts/nav-context";
import type { SyncConfirmDetail, SyncVerdict } from "./ui/sync-view";
import type { RestoreConfirmDetail, DeleteConfirmDetail, VersionScope, LocalStorageStatsLike } from "./ui/versions-view";
import type { RetentionRules as RetentionModelRules } from "./ui/retention-model";

// Wails RetentionRules is snake_case (keep_last…); the model/component speak
// camelCase. Map at the host boundary (design-log/039).
const toModelRules = (r: RetentionRules): RetentionModelRules => ({
    keepLast: r.keep_last,
    keepDaily: r.keep_daily,
    keepWeekly: r.keep_weekly,
    keepMonthly: r.keep_monthly,
});
const toBindingRules = (m: RetentionModelRules): RetentionRules =>
    new RetentionRules({
        keep_last: m.keepLast,
        keep_daily: m.keepDaily,
        keep_weekly: m.keepWeekly,
        keep_monthly: m.keepMonthly,
    });
import type { PrepSettings, PrepSettingsChangeDetail } from "./ui/prep-settings";

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
    // Effective bytes-per-second for the current transfer beat. Single source
    // of truth shared between the under-slot speed cell and the ETA — they
    // can never disagree. Derivation lives in applyVm() (see computeSpeedBps)
    // and lands on @state speedBps so render is a pure function of reactive
    // state — design-log/020 §Class B.
    effectiveSpeedBps: number;
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

function isTransferPhase(phase: Phase): boolean {
    return phase === Phase.PhaseDownloading || phase === Phase.PhaseSaving;
}

function arcFromBytes(vm: ViewModel): number {
    // Empty-delta transfer: the pre-flight list (design-log/019) found
    // every blob already at the destination, so PlanInfo announces
    // bytesTotal == 0 and no Tick fires. Projection sets progress = 100
    // in that case so the dial reads complete-on-arrival instead of
    // sticking at zero. Pre-PlanInfo state also has bytesTotal == 0 but
    // progress == 0, which arcFromBytes maps to 0 — no flash.
    if (vm.bytesTotal <= 0) return Math.max(0, Math.min(1, vm.progress / 100));
    return Math.min(1, Math.max(0, vm.bytesDone / vm.bytesTotal));
}

// ETA reads vm.etaSeconds directly — computed Go-side from the beat-wide
// average rate and already monotone non-increasing within a beat
// (design-log/028). No division here: deriving it from the volatile
// effectiveSpeedBps (the under-slot speed number) is exactly what made the
// estimate swing "4min → 30s → 2min in 3 seconds". 0 means "no estimate yet"
// — first tick of a beat, empty plan, or non-transfer phase — and renders as
// the decoder placeholder (design-log/009 §Q5), not a fake number.
function etaSub(vm: ViewModel, _ctx: AppCtx): string {
    // No transfer plan yet (bytesTotal == 0): we are in a non-byte stage —
    // Checking, or a sync flow's prep/commit/retain beat (design-log/031).
    // Return "" so the dial shows no sub at all, NOT the `·····` placeholder,
    // which decodes fast and would jitter for the whole non-transfer span.
    if (vm.bytesTotal <= 0) return "";
    // Plan complete (all bytes out): the calm save-tail caption, letters only.
    if (vm.bytesDone >= vm.bytesTotal) return "Almost done";
    // Plan live but no estimate yet — the brief first-tick grace window
    // (design-log/009 §Q5): the placeholder decode is intentional here.
    if (vm.etaSeconds <= 0) return formatEta(null);
    return formatEta(vm.etaSeconds);
}

// Phase → dial view table. Single source of truth for glyph + label + arc +
// sub-line + under-slot dispatch. Per design-log/017 §Visual dispatch +
// copy table. Lock-conflict is a PhaseFailed beat with friendly copy
// resolved at render time (sees vm.lockHolder).
const PHASE_VIEW: Record<Phase, DialView> = {
    [Phase.$zero]: {
        state: "idle", glyph: "play", label: "Start", underSlot: null,
        arc: () => 0,
        // Staleness is no longer surfaced on the dial (design-log/034): the
        // Files → Sync view reports it on demand. The resting dial stays calm.
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
        // Save-tail per design-log/017: once all bytes are out, etaSub returns
        // "Almost done" (the arc-plateau housekeeping beat). The label still
        // swaps to "Wrapping up" in derive() on the same bytes-complete test.
        sub: etaSub,
    },
    [Phase.PhaseFailed]: {
        state: "fail", glyph: "x", label: "",
        // Failure label resolved at render time so locked-conflict and
        // generic failures pick distinct copy from the same dispatch slot.
        underSlot: null,
        arc: (_vm, ctx) => ctx.lastProgressArc,
        sub: () => "Tap to dismiss",
    },
    // Autoupdate (design-log/037): gray, inert, no glyph — "system working,
    // hands off". Full gray ring signals busy (indeterminate; no byte
    // denominator, §Q7). Preflight = the launch/recheck probe; Updating = the
    // new binary downloading, captioned with the target version.
    [Phase.PhasePreflight]: {
        state: "preflight", glyph: null, label: "", underSlot: null,
        arc: () => 1,
        sub: () => "Checking for updates",
    },
    [Phase.PhaseUpdating]: {
        state: "preflight", glyph: null, label: "", underSlot: null,
        arc: () => 1,
        sub: (vm) => `Updating → v${vm.targetVersion}`,
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
    // Passive IDLE cue (design-log/035 §Q6 + OQ1): true when local work isn't
    // safely canonical (dirty || unpushed). Folded from getSyncStatus on mount;
    // drives a muted <rune-decoder> "Unpublished changes" below the Advanced
    // link. Empty string is never handed to the decoder (design-log/020/028).
    @state() private unpublished = false;
    // Transient skip-sync toggle mirrored from <prep-settings> (design-log/036
    // §Q6). Read at Start time; not persisted. Tracked here because the live
    // form lives in a pushed pane and isn't query-able on the dial tap.
    @state() private skipSync = false;
    // Snapshot of effective transfer rate, derived once in applyVm() so the
    // under-slot speed and the ETA always see the same number. Keeping the
    // derivation off the render path keeps render() a pure function of
    // reactive state — design-log/020 §Class B.
    @state() private speedBps = 0;
    @query("rune-stack") private _stack!: RuneStack | null;
    private runStartedAt = 0;
    private uptimeTimer = 0;
    private unsubscribe?: () => void;
    // Transfer-rate fallback anchors. transferStartedAt = wall time the
    // current Downloading/Saving beat began; transferStartBytes = the
    // bytesDone value at that moment (non-zero on resume scenarios where
    // the projection hasn't reset the counter). effectiveSpeedBps() takes
    // (bytesDone - transferStartBytes) / (now - transferStartedAt) when
    // vm.logicalMbps is 0 — keeps the speed cell + ETA honest under fast
    // transfers or pre-Tick windows where the Go-side rolling average
    // hasn't settled. Reset to 0 / 0 outside transfer phases so a stale
    // start time can't bleed into a new beat.
    private transferStartedAt = 0;
    private transferStartBytes = 0;

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
        void this.refreshUnpublished();
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
        const wasIdle = this.vm.phase === Phase.PhaseIdle;
        const isIdle = vm.phase === Phase.PhaseIdle;
        // Order matters (design-log/020 §Class C): refresh the transfer-beat
        // anchors and the speed snapshot BEFORE the arc/ctx read so the arc
        // is computed against the new beat, not the stale one.
        this.trackTransferBeat(vm);
        this.speedBps = this.computeSpeedBps(vm);
        if (vm.phase !== Phase.PhaseFailed) {
            this.lastNonFailPhase = vm.phase;
            this.lastProgressArc = PHASE_VIEW[vm.phase]?.arc(vm, this.ctx()) ?? 0;
        }
        if (isPlaying && !wasPlaying) this.startUptime();
        if (!isPlaying && wasPlaying) this.stopUptime();
        this.vm = vm;
        if (!wasIdle && isIdle) void this.refreshUnpublished();
    }

    // trackTransferBeat snapshots wall time + bytesDone at the moment a
    // Downloading or Saving beat begins, and clears the anchor on exit.
    // Used by effectiveSpeedBps() to compute a fallback rate when the
    // Go-side rolling DataAverage hasn't settled. Treats Downloading and
    // Saving as separate beats (each gets its own start) so a save that
    // follows a download doesn't inherit the download's elapsed time.
    private trackTransferBeat(vm: ViewModel) {
        const wasTransfer = isTransferPhase(this.vm.phase);
        const isTransfer = isTransferPhase(vm.phase);
        const beatChanged = wasTransfer && isTransfer && this.vm.phase !== vm.phase;
        if (isTransfer && (!wasTransfer || beatChanged)) {
            this.transferStartedAt = performance.now();
            this.transferStartBytes = vm.bytesDone;
        } else if (!isTransfer) {
            this.transferStartedAt = 0;
            this.transferStartBytes = 0;
        }
    }

    // Pure function of (vm, transfer-beat anchors, performance.now()). Called
    // from applyVm() so render never reads wall-clock — design-log/020 §B.
    private computeSpeedBps(vm: ViewModel): number {
        if (vm.logicalMbps > 0) return vm.logicalMbps * MBPS_TO_BPS;
        if (this.transferStartedAt === 0) return 0;
        const elapsedS = (performance.now() - this.transferStartedAt) / 1000;
        // 100 ms floor: any shorter window and a single buffered Read can
        // pin the divisor close to zero and produce nonsense MB/s spikes.
        if (elapsedS <= 0.1) return 0;
        const flowed = Math.max(0, vm.bytesDone - this.transferStartBytes);
        if (flowed <= 0) return 0;
        return flowed / elapsedS;
    }

    private ctx(): AppCtx {
        return {
            uptimeSub: this.uptimeSub,
            lastProgressArc: this.lastProgressArc,
            lastNonFailPhase: this.lastNonFailPhase,
            effectiveSpeedBps: this.speedBps,
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
            speedBps: ctx.effectiveSpeedBps,
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

        // A wire stall — projection sets vm.stalled when the link goes quiet
        // mid-transfer (a quiet R2 PutStream, surfaced via the ticker's
        // heartbeat) — overrides the ETA sub with an honest waiting caption so
        // the dial reads live-but-waiting rather than a silently frozen ETA.
        // vm.stalled is only ever true during a transfer phase. Design-log/022 #2.
        const sub = vm.stalled ? "Stalled — waiting on R2…" : view.sub(vm, ctx);

        return {
            dial: {
                state: view.state,
                arc: view.arc(vm, ctx),
                glyph: view.glyph,
                label,
                sub,
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
        // Settings live in a pushed pane now (design-log/034), so the live
        // <prep-settings> element isn't query-able from here — Start reads the
        // last valid values tracked into `this.prep` via onPrepChange, plus the
        // transient skip-sync toggle (design-log/036).
        void start(this.prep.port, this.prep.memoryMB, this.skipSync);
    };

    // ── Advanced navigation tenant (design-log/034) ─────────────────────────
    // One flat staged pane pushed from the quiet IDLE "advanced" link: two
    // sections, Server (port/memory, ex-inline disclosure 014/029) and Sync
    // (031 gestures). No menu, no nesting. The child events bubble up here:
    // `change` tracks the latest valid settings, `sync` runs the gesture.
    private advancedView: NavView = {
        id: "advanced",
        title: "advanced",
        render: () => html`<advanced-view
            .config=${this.prep}
            ?skipSync=${this.skipSync}
            .check=${this.checkSync}
            .versions=${this.listVersions}
            .versionStats=${this.loadLocalStats}
            ?dirty=${this.unpublished}
            .loadRules=${this.loadRetention}
            .canUpdate=${this.vm.phase === Phase.PhaseIdle}
            @change=${this.onPrepChange}
            @sync=${this.onSyncConfirmed}
            @checkupdate=${this.onCheckUpdate}
            @restore=${this.onRestoreConfirmed}
            @delete=${this.onDeleteConfirmed}
            @publishfirst=${this.onPublishFirst}
            @retentionchange=${this.onRetentionChange}
            @retentionapply=${this.onRetentionApply}
        ></advanced-view>`,
    };

    // Version listing injected into <versions-view>, scope-aware
    // (design-log/045 §B). The backend lister degrades remote→local on offline
    // failure (design-log/038 §Q2) but with explicit tabs the user can also
    // choose Local directly. Bound so `this` is stable across renders.
    private listVersions = (scope: VersionScope) => listVersions(scope);

    // Local on-disk stats fetcher injected into <versions-view> Local-tab
    // header (design-log/045 §E). Cached 5s server-side; invalidates on
    // delete + apply success.
    private loadLocalStats = async (): Promise<LocalStorageStatsLike> => {
        const s = await getLocalStorageStats();
        return { bytesOnDisk: s.bytesOnDisk, objectCount: s.objectCount };
    };

    // Retention rules load + persist (design-log/039). The Wails RetentionRules
    // model is snake_case (keep_last…); the retention-model/component speak
    // camelCase — so map at this boundary. The preview is illustrative (033
    // §Redesign), so no backup history is fed in.
    private loadRetention = async (): Promise<{ local: RetentionModelRules; remote: RetentionModelRules }> => {
        const c = await getRetentionRules();
        return { local: toModelRules(c.local), remote: toModelRules(c.remote) };
    };

    // Retention `change` is now a fine-grained signal that fires on every
    // stepper tap (design-log/045 §D / §Q6 decided: staged edits). It carries
    // the *staged* draft, but the host does not persist it here — auto-save
    // is gone. Persistence happens on the user's explicit Apply press
    // (onRetentionApply below). Keeping this listener as a no-op so the
    // payload remains inspectable for future use and the existing test
    // contract (advanced-view re-emits as `retentionchange`) is unchanged.
    private onRetentionChange = (_e: CustomEvent<{ local: RetentionModelRules; remote: RetentionModelRules }>) => {
        // Intentional no-op (design-log/045 §D).
    };

    private openAdvanced = () => this._stack?.push(this.advancedView);

    // Manual update check: pop back to the root so the gray Preflight takeover
    // is visible on the dial, then trigger the same flow as launch (037 §Q6).
    private onCheckUpdate = () => {
        this._stack?.popToRoot();
        checkForUpdate();
    };

    // Settings (port/memory) live in the staged pane now, so the live form
    // isn't query-able on Start — track the last valid values into this.prep.
    private onPrepChange = (e: CustomEvent<PrepSettingsChangeDetail>) => {
        if (e.detail.valid && e.detail.settings) this.prep = e.detail.settings;
        // Mirror the transient skip-sync toggle regardless of field validity
        // (design-log/036 §Q6) — it's independent of port/memory.
        this.skipSync = e.detail.skipSync;
    };

    // HEAD probe injected into <sync-view>. getSyncStatus merges the head
    // compare with the workdir-dirty scan (design-log/035). `ahead` now means
    // "any uncanonical local state to publish" — dirty edits OR a committed-but-
    // unpushed ref (§Q7) — not just a newer local HEAD. `behind` no longer
    // gates the offer; it only adds a loud warning in sync-view. `dirty` is
    // forwarded too so the Revert confirm (design-log/045 §C) can pick honest
    // copy (drops edits vs no-op refresh). Errors propagate so sync-view shows
    // "Couldn't reach the remote".
    private checkSync = async (): Promise<SyncVerdict> => {
        const s = await getSyncStatus();
        return {
            behind: s.behind,
            ahead: s.dirty || s.unpushed,
            dirty: s.dirty,
        };
    };

    // Fold the launch staleness check into the IDLE "Unpublished changes" cue
    // (design-log/035 §Q6). Degrades silently to no cue on any error — the
    // backend already collapses failures to a zero status (design-log/031 OQ3).
    private async refreshUnpublished() {
        try {
            const s = await getSyncStatus();
            this.unpublished = s.dirty || s.unpushed;
        } catch {
            this.unpublished = false;
        }
    }

    // Confirmed Download/Upload (design-log/031) + Revert (design-log/045 §C).
    // All three flows animate the root dial via the onView stream, so unwind
    // the stack to the dial to watch them run.
    private onSyncConfirmed = (e: CustomEvent<SyncConfirmDetail>) => {
        switch (e.detail.direction) {
            case "download":
                void download();
                break;
            case "upload":
                void upload();
                break;
            case "revert":
                void revert();
                break;
        }
        this._stack?.popToRoot();
    };

    // Confirmed restore (design-log/038). Like a Download it animates the root
    // dial via the onView stream (reuses the download beat — FlowRestore), so
    // unwind the stack to watch it run. HEAD never moves; the reverted workdir
    // then reads as dirty and is recoverable via Publish.
    private onRestoreConfirmed = (e: CustomEvent<RestoreConfirmDetail>) => {
        void restore(e.detail.refID);
        this._stack?.popToRoot();
    };

    // Confirmed per-version delete (design-log/045 §A). Fast and local — no
    // dial takeover; the versions-view re-loads its listing optimistically so
    // the row vanishes immediately. Errors raise no UI; the row reappears on
    // the next refresh, which is the honest "still here" signal.
    private onDeleteConfirmed = (e: CustomEvent<DeleteConfirmDetail>) => {
        void deleteLocalVersion(e.detail.refID);
    };

    // Apply retention (design-log/045 §D). The advanced-view re-emits the
    // inner `<retention-rules>` `apply` event with the {local, remote} payload
    // — persist first via setRetentionRules so the next prune reads the
    // freshly-saved policy ([[039]] §Q1 reads-fresh-at-Select), then publish
    // ApplyRetentionRequested to run the prune. Animates the dial via
    // FlowRetentionApply (gray ring — reuses PhasePreflight) so pop the stack
    // to watch it. The two calls are serialised: applyRetentionNow waits on
    // setRetentionRules so the prune cannot read stale settings.
    private onRetentionApply = async (e: CustomEvent<{ local: RetentionModelRules; remote: RetentionModelRules }>) => {
        try {
            await setRetentionRules(toBindingRules(e.detail.local), toBindingRules(e.detail.remote));
            void applyRetentionNow();
        } finally {
            this._stack?.popToRoot();
        }
    };

    // "Publish first" nudge from the restore confirm when the workdir is dirty
    // (design-log/038 §Q6): publish the current state, then return to the dial.
    // The user re-enters Versions to roll back once their work is safe.
    private onPublishFirst = () => {
        void upload();
        this._stack?.popToRoot();
    };

    private onHoldCommit = () => {
        if (this.vm.phase === Phase.PhasePlaying) void stop();
    };

    // Whether the current failure happened in a sync stage (design-log/036 §Q7).
    // Only these warrant the "Skip sync & run locally" escape hatch — retrying
    // the identical sync would just fail again, but a local-only run can still
    // launch. Non-sync failures (the server itself: Running/port/OOM) get only
    // dismiss-to-idle.
    //
    // LIMITATION: the projection ViewModel does NOT expose the fine-grained
    // ritual stage (Checking/Pulling/Acquiring/Pushing). Its `Stage`/`Phase`
    // enums are the coarse 017 UI buckets, and on failure `vm.phase` collapses
    // to PhaseFailed. So we gate on `lastNonFailPhase` — the bucket the run was
    // in just before it failed. The sync-bearing buckets are `downloading`
    // (Checking/Pulling/Acquiring run here) and `saving` (Pushing). A failure
    // during `playing` is the server crashing/port-in-use → no hint; `preparing`
    // (spin-up after the world is in place) is also non-sync → no hint.
    private failedInSyncStage(): boolean {
        if (this.vm.phase !== Phase.PhaseFailed) return false;
        // A lock-held conflict is a legitimate "someone is playing" gate, not a
        // sync-transport failure — never offer skip-sync there.
        if (this.vm.lockHolder) return false;
        return (
            this.lastNonFailPhase === Phase.PhaseDownloading ||
            this.lastNonFailPhase === Phase.PhaseSaving
        );
    }

    // FAILED-view "Skip sync & run locally" (design-log/036 §Q7): re-fire Start
    // with skipSync=true, reusing the last-entered params. Additive beside the
    // dial's dismiss-to-idle tap.
    private onSkipSyncRetry = (e: Event) => {
        e.stopPropagation();
        void start(this.prep.port, this.prep.memoryMB, true);
    };

    // RUN-stage console affordance (design-log/043 Part 2): the only path to the
    // logs window. <run-console-link> emits `press`; ShowLogs builds the window
    // lazily and the console backfills from latest.log on open.
    private onOpenConsole = () => void showLogs();

    private underSlotChild(d: Derived) {
        if (d.underSlot === "addresses") {
            // Addresses + the console clickyclacky share the playing under-slot
            // (design-log/043 Part 2), stacked in one cluster.
            return html`<div class="run-cluster">
                <run-addresses .addresses=${d.addresses}></run-addresses>
                <run-console-link @press=${this.onOpenConsole}></run-console-link>
            </div>`;
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
        // rune-stack is the app root: the dial screen is the root pane (default
        // slot); Files / Sync are pushed panes that slide the whole screen left
        // (design-log/034). No modals anywhere.
        return html`
            <rune-stack>
                <ritual-shell .state=${d.dial.state}>
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
                        ? html`<button class="advanced-entry" @click=${this.openAdvanced}>
                                  <span class="chev">›</span> Advanced
                              </button>
                              ${this.unpublished
                                  ? html`<rune-decoder
                                        class="unpublished-cue"
                                        .text=${"Unpublished changes"}
                                    ></rune-decoder>`
                                  : null}`
                        : null}
                    ${this.failedInSyncStage()
                        ? html`<button class="skip-sync-hint" @click=${this.onSkipSyncRetry}>
                              Skip sync &amp; run locally
                          </button>`
                        : null}
                </ritual-shell>
            </rune-stack>
        `;
    }

    static styles = css`
        :host { display: contents; }
        rune-stack { display: block; height: 100vh; }

        /* Quiet ambient affordance — same low-attention register as the footer
           links; sits where the old inline "Advanced" disclosure did. */
        .advanced-entry {
            align-self: center;
            background: none;
            border: 0;
            padding: var(--space-2);
            cursor: pointer;
            color: var(--text-faint);
            font-family: var(--font-mono);
            font-size: var(--fs-caption);
            letter-spacing: 0.12em;
            text-transform: uppercase;
            display: inline-flex;
            align-items: center;
            justify-content: center;
            gap: var(--space-2);
            transition: color var(--motion-fast, 120ms ease);
        }
        .advanced-entry:hover { color: var(--text-muted); }
        .advanced-entry:focus-visible {
            outline: 1px solid var(--text-muted);
            outline-offset: 2px;
            border-radius: var(--radius-sm);
        }
        .advanced-entry .chev { font-size: var(--fs-body); line-height: 1; }

        /* Passive "Unpublished changes" cue (design-log/035 §Q6) — muted, below
           the Advanced link, no button. Points the operator into Advanced → Sync. */
        .unpublished-cue {
            align-self: center;
            color: var(--text-faint);
            font-family: var(--font-mono);
            font-size: var(--fs-caption);
            letter-spacing: 0.08em;
        }

        /* Additive FAILED-view escape hatch (design-log/036 §Q7), shown only for
           sync-stage failures. Same quiet register as the Advanced link; the
           dismiss-to-idle tap on the dial stays the default action. */
        .skip-sync-hint {
            align-self: center;
            background: none;
            border: 1px solid var(--text-faint);
            border-radius: var(--radius-sm);
            padding: var(--space-2) var(--space-3);
            cursor: pointer;
            color: var(--text-muted);
            font-family: var(--font-mono);
            font-size: var(--fs-caption);
            letter-spacing: 0.08em;
            transition: color var(--motion-fast, 120ms ease), border-color var(--motion-fast, 120ms ease);
        }
        .skip-sync-hint:hover { color: var(--text-strong); border-color: var(--text-muted); }
        .skip-sync-hint:focus-visible {
            outline: 1px solid var(--text-muted);
            outline-offset: 2px;
        }
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
        /* Playing under-slot: addresses with the console clickyclacky set apart
           beneath them (design-log/043 Part 2). */
        .run-cluster {
            display: flex;
            flex-direction: column;
            align-items: center;
            gap: var(--space-6);
            width: 100%;
        }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        "ritual-app": RitualApp;
    }
}
