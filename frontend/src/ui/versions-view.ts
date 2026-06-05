/**
 * Versions view — world-save rollback (design-log/038) + per-version delete +
 * Local/Remote tabs + total-on-disk header (design-log/045). Lists historical
 * refs newest-first; tapping an older row reveals an inline two-step Restore
 * confirm; tapping the × on a Local-tab row reveals an inline two-step Delete
 * confirm (no dialog/popup/toast — the user's brief).
 *
 * Non-destructive Restore framing (design-log/035 "user data is sacred"): a
 * restore never deletes a version or moves HEAD — it brings an older world
 * back into the workdir, which then reads as dirty and is recoverable via
 * Publish. The only thing at risk is *unsaved* edits, so when `dirty` the
 * confirm offers a "Publish first" escape (emits `publishfirst`) above
 * Restore — a non-blocking nudge, never a gate.
 *
 * Delete is *destructive on the local cache only* (design-log/045 §Q10). The
 * confirm copy says exactly that: the remote keeps its copy; Download brings
 * it back. The user can delete any row including the loaded one and HEAD
 * (design-log/045 §Q2) — the confirm spells out the sharp edge case-by-case.
 *
 * Local · Remote tabs (design-log/045 §B): `<rune-segmented>` at the top of
 * the panel, defaults to Local. Selecting a tab re-runs the injected
 * `list(scope)` and re-reads the stats (Local only). Delete + the on-disk
 * header live on the Local tab; the Remote tab is read-only.
 *
 * Presentational: the listing is injected via `.list(scope)` (host wraps
 * `listVersions(scope)`); confirming Restore emits `restore { refID }`,
 * confirming Delete emits `delete { refID }` for the host to run. Date / size
 * / total strings are computed at load time, never in render(), so render
 * stays a pure function of state (design-log/020).
 *
 * HIG — progressive in-context disclosure over modal confirmation:
 * https://developer.apple.com/design/human-interface-guidelines/navigation-and-search
 */

import { LitElement, css, html, nothing } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import "./primitives/rune-button";
import "./primitives/rune-row";
import "./primitives/rune-segmented";
import "./primitives/decoder";

/** Structural shape of one version row — compatible with the Wails `Version`
 * model so the host can pass `listVersions(scope)` results straight through.
 * `isLoaded` (design-log/044) marks the row the workdir reflects — that is
 * what the "current" badge follows now, not isHead. */
export interface VersionRow {
    id: string;
    unixMs: number;
    files: number;
    sizeBytes: number;
    isHead: boolean;
    isLoaded: boolean;
    source: string;
}

/** Local on-disk stats injected by the host (design-log/045 §E). Dedup-aware
 * byte sum + object count under local `objects/`. */
export interface LocalStorageStatsLike {
    bytesOnDisk: number;
    objectCount: number;
}

export type VersionScope = "local" | "remote";

export interface RestoreConfirmDetail {
    refID: string;
}

export interface DeleteConfirmDetail {
    refID: string;
}

type LoadPhase = "loading" | "loaded" | "error";

// Two pending-confirm modes share the same row context. `kind` discriminates
// so render picks the right copy + buttons; the row reference is just the
// already-prepared DisplayRow.
interface PendingConfirm {
    kind: "restore" | "delete";
    row: DisplayRow;
}

// One row prepared for render — strings formatted once at load so render() does
// no Date()/Intl work (design-log/020 purity). `isLoaded` (design-log/044)
// drives the "current" badge; `isHead` flags HEAD so the Delete-confirm copy
// can warn about deleting the newest ref. Both are needed for the Local-tab
// Delete affordance to render the right confirm text.
interface DisplayRow {
    id: string;
    isHead: boolean;
    isLoaded: boolean;
    dateLabel: string;
    metaLabel: string;
}

const DATE_FMT = new Intl.DateTimeFormat(undefined, {
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
});

const SCOPE_OPTS = [
    { value: "local", label: "Local" },
    { value: "remote", label: "Remote" },
];

function formatBytes(n: number): string {
    if (n <= 0) return "0 B";
    const units = ["B", "KB", "MB", "GB", "TB"];
    const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
    const v = n / 1024 ** i;
    return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`;
}

// Build the "N versions · X on disk" caption + the dedup-aware hint. The hint
// fires only when the logical-sum is materially larger than the on-disk number
// (>1.5×), otherwise the line would be noise on a fresh single-version store
// (design-log/045 §E §Q9). Returns empty header when there are no rows AND no
// blobs — nothing useful to say.
function buildStatsHeader(
    rows: DisplayRow[],
    stats: LocalStorageStatsLike | null,
    logicalSumBytes: number,
): { headline: string; hint: string } {
    if (!stats) return { headline: "", hint: "" };
    const versions = rows.length;
    const verb = versions === 1 ? "version" : "versions";
    const headline = `${versions} ${verb} · ${formatBytes(stats.bytesOnDisk)} on disk`;
    const dedupRatio = stats.bytesOnDisk > 0 ? logicalSumBytes / stats.bytesOnDisk : 0;
    const hint =
        versions > 1 && dedupRatio > 1.5 ? "Shared content keeps disk use small." : "";
    return { headline, hint };
}

@customElement("versions-view")
export class VersionsView extends LitElement {
    /** Version listing per scope, injected by the host (wraps
     * `listVersions(scope)`). Re-runs on every tab switch. */
    @property({ attribute: false }) list: (scope: VersionScope) => Promise<VersionRow[]> = async () => [];

    /** Local-store on-disk stats fetcher, injected by the host (design-log/045
     * §E). Only called when the Local tab is active. nil-equivalent (returns
     * zero) is harmless — the header just renders 0 B. */
    @property({ attribute: false }) stats: () => Promise<LocalStorageStatsLike> = async () => ({
        bytesOnDisk: 0,
        objectCount: 0,
    });

    /** Workdir has unsaved changes — surfaces the "Publish first" nudge in the
     * Restore confirm (design-log/038 §Q6). Has no effect on Delete confirms. */
    @property({ type: Boolean }) dirty = false;

    @state() private _scope: VersionScope = "local";
    @state() private _phase: LoadPhase = "loading";
    @state() private _rows: DisplayRow[] = [];
    @state() private _logicalSum = 0;
    @state() private _stats: LocalStorageStatsLike | null = null;
    @state() private _pending: PendingConfirm | null = null;

    // Monotonic counter for #load — every call bumps it and snapshots the
    // bumped value as its "epoch". When the async list/stats promises settle,
    // they compare against the live counter: if it has advanced, a newer
    // #load fired in the meantime and this one's result is stale. Drops the
    // stale result on the floor instead of stomping on the fresh state.
    // Without this, rapid tab toggles (Remote → Local → Remote → Local) race
    // and a slow Remote response can land after the user is on Local,
    // overwriting Local rows with Remote ones.
    private _loadEpoch = 0;

    static styles = css`
        :host {
            display: block;
            font-family: var(--font-mono);
            color: var(--text);
        }
        .panel {
            display: flex;
            flex-direction: column;
            gap: var(--space-3);
        }
        .tabs {
            display: flex;
        }
        .header {
            display: flex;
            flex-direction: column;
            gap: 2px;
            color: var(--text-muted);
            font-size: var(--fs-caption);
            min-height: 1.2em;
        }
        .header .headline {
            color: var(--text);
        }
        .header .hint {
            color: var(--text-faint);
        }
        .status {
            display: block;
            min-height: 1.4em;
            color: var(--text-muted);
        }
        .rows {
            display: flex;
            flex-direction: column;
            gap: var(--space-1);
            max-height: 320px;
            overflow-y: auto;
        }
        .date {
            color: var(--text-strong);
        }
        .meta {
            color: var(--text-muted);
            font-size: var(--fs-caption);
        }
        .badge {
            color: var(--state-run, #3fb950);
            font-size: var(--fs-caption);
            letter-spacing: 0.08em;
            text-transform: uppercase;
        }
        /* Row + delete pair. We do NOT nest the delete button inside the
           pressable rune-row (HTML disallows button-in-button + ARIA treats
           rune-row as role=button when pressable). Instead the row and the
           delete affordance are siblings under .row-pair; the row owns Restore
           via @press, the × owns Delete via @click. Per user direction
           2026-06-05. */
        .row-pair {
            display: grid;
            grid-template-columns: 1fr auto;
            align-items: center;
            gap: var(--space-1);
        }
        .del {
            background: none;
            border: 0;
            color: var(--text-faint);
            font-family: var(--font-mono);
            font-size: var(--fs-body);
            cursor: pointer;
            padding: var(--space-1) var(--space-2);
            line-height: 1;
            transition: color var(--motion-fast, 120ms ease);
        }
        .del:hover {
            color: var(--warning, #e0a106);
        }
        .del:focus-visible {
            outline: 1px solid var(--text-muted);
            outline-offset: 2px;
            border-radius: var(--radius-sm);
        }
        .del:disabled {
            opacity: 0.35;
            cursor: not-allowed;
        }
        .confirm-body {
            margin: 0;
            color: var(--text-muted);
            line-height: 1.6;
        }
        .warn {
            margin: 0;
            padding: var(--space-3);
            border-radius: var(--radius-sm);
            color: var(--warning, #e0a106);
            background: color-mix(in srgb, var(--warning, #e0a106) 12%, transparent);
            border: 1px solid color-mix(in srgb, var(--warning, #e0a106) 40%, transparent);
            line-height: 1.5;
        }
        .actions {
            display: flex;
            flex-direction: column;
            gap: var(--space-3);
        }
        .confirm-actions {
            display: grid;
            grid-template-columns: 1fr 1fr;
            gap: var(--space-3);
        }
    `;

    firstUpdated() {
        // Lazy remount on each Advanced navigation re-probes the listing, so the
        // history is never stale and the glitch-decode plays as the pane slides
        // in (mirrors sync-view, design-log/034). Default scope is Local
        // (design-log/045 §Q4 decided).
        void this.#load();
    }

    render() {
        return html`<div class="panel">
            <div class="tabs">
                <rune-segmented
                    .options=${SCOPE_OPTS}
                    value=${this._scope}
                    label="Versions scope"
                    @change=${this.#onScope}
                ></rune-segmented>
            </div>
            ${this._pending
                ? this._pending.kind === "restore"
                    ? this.#renderRestoreConfirm(this._pending.row)
                    : this.#renderDeleteConfirm(this._pending.row)
                : this.#renderList()}
        </div>`;
    }

    #renderList() {
        if (this._phase === "loading")
            return html`<rune-decoder class="status" .text=${"Loading versions…"}></rune-decoder>`;
        if (this._phase === "error")
            return html`
                <rune-decoder class="status" .text=${"Couldn't load versions."}></rune-decoder>
                <rune-button variant="tinted" @press=${this.#load}>Try again</rune-button>
            `;
        if (this._rows.length === 0)
            return html`<rune-decoder class="status" .text=${"No earlier versions yet."}></rune-decoder>`;
        // Stats header only renders on the Local tab, and only when stats
        // resolved (design-log/045 §E suppresses remote stats — counts there are
        // not actionable, and an R2 List+Stat sweep costs real money).
        const header =
            this._scope === "local"
                ? buildStatsHeader(this._rows, this._stats, this._logicalSum)
                : { headline: "", hint: "" };
        return html`
            ${header.headline
                ? html`<div class="header">
                      <rune-decoder class="headline" .text=${header.headline}></rune-decoder>
                      ${header.hint
                          ? html`<rune-decoder class="hint" .text=${header.hint}></rune-decoder>`
                          : nothing}
                  </div>`
                : nothing}
            <div class="rows">${this._rows.map((r) => this.#renderRow(r))}</div>
        `;
    }

    #renderRow(r: DisplayRow) {
        // The currently-loaded version is not a restore target (restoring what
        // is already in the workdir is a no-op) — show the badge, no press
        // affordance. Other rows are pressable and open the Restore confirm.
        // After a Restore the loaded row is NOT HEAD; the badge follows the
        // workdir (design-log/044), so "current" lands on the restored row.
        //
        // The × delete affordance is a SIBLING of the rune-row, not nested
        // inside its trailing slot — buttons cannot be nested (HTML disallows
        // button-in-button + rune-row is role=button when pressable). The
        // .row-pair wrapper aligns them in one visual line.
        const isLocal = this._scope === "local";
        const pressable = !r.isLoaded;
        const row = html`<rune-row
            ?pressable=${pressable}
            aria-label=${r.isLoaded ? `${r.dateLabel} (current)` : `Restore ${r.dateLabel}`}
            @press=${pressable ? () => this.#askRestore(r) : nothing}
        >
            <span slot="leading" class="date">${r.dateLabel}</span>
            <span class="meta">${r.metaLabel}</span>
            ${r.isLoaded
                ? html`<span slot="trailing" class="badge">current</span>`
                : html`<span slot="trailing" class="meta">${isLocal ? "" : "›"}</span>`}
        </rune-row>`;
        if (!isLocal) return row;
        return html`<div class="row-pair">
            ${row}
            <button
                class="del"
                aria-label=${`Delete ${r.dateLabel}`}
                @click=${(e: Event) => this.#onDeleteClick(e, r)}
            >×</button>
        </div>`;
    }

    #renderRestoreConfirm(r: DisplayRow) {
        return html`
            <p class="confirm-body">
                Bring back the world from ${r.dateLabel}. Your current world isn't deleted —
                publish this restored state afterward to keep it.
            </p>
            ${this.dirty
                ? html`<p class="warn">
                          You have unsaved changes. Restoring replaces them — publish first to
                          keep them.
                      </p>
                      <div class="actions">
                          <rune-button variant="tinted" @press=${this.#publishFirst}>Publish first</rune-button>
                      </div>`
                : nothing}
            <div class="confirm-actions">
                <rune-button variant="tinted" @press=${this.#cancel}>Cancel</rune-button>
                <rune-button variant="primary" @press=${this.#confirmRestore}>Restore</rune-button>
            </div>
        `;
    }

    // Delete confirms come in three flavours per the design log (§Q2):
    //   - loaded row that is also HEAD: warns that the local store loses its
    //     anchor; recovery via Publish/Download.
    //   - loaded row that isn't HEAD (post-Restore): warns about losing the
    //     restored anchor; workdir reads as dirty afterward.
    //   - plain non-loaded row: emphasises the remote keeps the copy.
    #renderDeleteConfirm(r: DisplayRow) {
        let body: string;
        let warn: string | null = null;
        if (r.isLoaded && r.isHead) {
            body = "Delete this saved version? Your workdir files stay, but the local store loses the ref it was anchored to — Publish or Download to get a clean reference again.";
            warn = "This is your current version.";
        } else if (r.isLoaded) {
            body = "Delete this saved version? Your workdir files stay, but the anchor is gone — the workdir will read as dirty. Publish or Download to recover.";
            warn = "You're currently on this older version.";
        } else {
            body = "Delete this local copy? The remote keeps it — Download will bring it back if you change your mind.";
        }
        return html`
            ${warn ? html`<p class="warn">${warn}</p>` : nothing}
            <p class="confirm-body">${body}</p>
            <div class="confirm-actions">
                <rune-button variant="tinted" @press=${this.#cancel}>Cancel</rune-button>
                <rune-button variant="primary" @press=${this.#confirmDelete}>Delete</rune-button>
            </div>
        `;
    }

    #load = async () => {
        const epoch = ++this._loadEpoch;
        this._pending = null;
        this._phase = "loading";
        // Clear the stats slot eagerly on every load so a tab switch out of
        // Local immediately hides the on-disk header instead of waiting for
        // the awaited list to settle. Set fresh below if the new scope is
        // Local AND the stats fetch wins its epoch race.
        this._stats = null;
        try {
            const raw = await this.list(this._scope);
            if (epoch !== this._loadEpoch) return; // stale: a newer #load fired
            this._rows = raw.map((v) => ({
                id: v.id,
                isHead: v.isHead,
                isLoaded: v.isLoaded,
                dateLabel: v.unixMs > 0 ? DATE_FMT.format(new Date(v.unixMs)) : v.id,
                metaLabel: `${v.files} ${v.files === 1 ? "file" : "files"} · ${formatBytes(v.sizeBytes)}`,
            }));
            this._logicalSum = raw.reduce((acc, v) => acc + (v.sizeBytes || 0), 0);
            this._phase = "loaded";
            // Stats fetch is independent — failure leaves _stats null and the
            // header silently omits the line (design-log/045 §E). Same epoch
            // guard: a slow stats call must not stomp a newer load's result.
            if (this._scope === "local") {
                this.stats()
                    .then((s) => {
                        if (epoch !== this._loadEpoch) return;
                        this._stats = s;
                    })
                    .catch(() => {
                        if (epoch !== this._loadEpoch) return;
                        this._stats = null;
                    });
            }
        } catch {
            if (epoch !== this._loadEpoch) return;
            this._phase = "error";
        }
    };

    #onScope = (e: CustomEvent<{ value: string }>) => {
        const next = e.detail.value as VersionScope;
        if (next === this._scope) return;
        this._scope = next;
        void this.#load();
    };

    #askRestore = (r: DisplayRow) => {
        this._pending = { kind: "restore", row: r };
    };

    #onDeleteClick = (e: Event, r: DisplayRow) => {
        // Stop the row's own press from also firing a Restore — the × is
        // visually inside the trailing slot but semantically a separate action.
        e.stopPropagation();
        this._pending = { kind: "delete", row: r };
    };

    #cancel = () => {
        this._pending = null;
    };

    #confirmRestore = () => {
        const p = this._pending;
        this._pending = null;
        if (!p || p.kind !== "restore") return;
        this.dispatchEvent(
            new CustomEvent<RestoreConfirmDetail>("restore", {
                detail: { refID: p.row.id },
                bubbles: true,
                composed: true,
            }),
        );
    };

    #confirmDelete = () => {
        const p = this._pending;
        this._pending = null;
        if (!p || p.kind !== "delete") return;
        this.dispatchEvent(
            new CustomEvent<DeleteConfirmDetail>("delete", {
                detail: { refID: p.row.id },
                bubbles: true,
                composed: true,
            }),
        );
        // Optimistically re-load so the row vanishes immediately. If the
        // backend delete fails the host can re-emit a refresh by calling
        // refresh() — for now the row reappears on the next firstUpdated().
        void this.#load();
    };

    #publishFirst = () => {
        this._pending = null;
        this.dispatchEvent(new CustomEvent("publishfirst", { bubbles: true, composed: true }));
    };

    /** Host hook — call after a Delete/Publish/Restore so the listing re-reads
     * without remounting the pane. Public so the host can refresh on
     * cross-flow events (e.g. dial idle re-entry per [[044]]). */
    refresh() {
        void this.#load();
    }
}

declare global {
    interface HTMLElementTagNameMap {
        "versions-view": VersionsView;
    }
}
