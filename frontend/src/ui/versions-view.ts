/**
 * Versions view — world-save rollback (design-log/038) as a section of the
 * Advanced pane. Lists historical refs newest-first; tapping an older one
 * reveals an inline two-step confirm (no dialog/popup/toast — the user's
 * brief) and, on confirm, emits `restore { refID }` for the host to run.
 *
 * Non-destructive framing (design-log/035 "user data is sacred"): a restore
 * never deletes a version or moves HEAD — it brings an older world back into
 * the workdir, which then reads as dirty and is recoverable via Publish. The
 * only thing at risk is *unsaved* edits, so when `dirty` the confirm offers a
 * "Publish first" escape (emits `publishfirst`) above Restore — a non-blocking
 * nudge, never a gate.
 *
 * Presentational: the listing is injected via `.list` (host wraps
 * `listVersions`); confirming emits an event for the host to run and unwind the
 * stack. Free of `wails-api` so it is testable and Storybook-able across every
 * state. Date/size strings are computed at load time, never in render(), so
 * render stays a pure function of state (design-log/020).
 *
 * HIG — progressive in-context disclosure over modal confirmation:
 * https://developer.apple.com/design/human-interface-guidelines/navigation-and-search
 */

import { LitElement, css, html, nothing } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import "./primitives/rune-button";
import "./primitives/rune-row";
import "./primitives/decoder";

/** Structural shape of one version row — compatible with the Wails `Version`
 * model so the host can pass `listVersions()` results straight through. */
export interface VersionRow {
    id: string;
    unixMs: number;
    files: number;
    sizeBytes: number;
    isHead: boolean;
    source: string;
}

export interface RestoreConfirmDetail {
    refID: string;
}

type LoadPhase = "loading" | "loaded" | "error";

// One row prepared for render — strings formatted once at load so render() does
// no Date()/Intl work (design-log/020 purity).
interface DisplayRow {
    id: string;
    isHead: boolean;
    dateLabel: string;
    metaLabel: string;
}

const DATE_FMT = new Intl.DateTimeFormat(undefined, {
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
});

function formatBytes(n: number): string {
    if (n <= 0) return "0 B";
    const units = ["B", "KB", "MB", "GB", "TB"];
    const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
    const v = n / 1024 ** i;
    return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`;
}

@customElement("versions-view")
export class VersionsView extends LitElement {
    /** Version listing, injected by the host (wraps `listVersions(scope)`). */
    @property({ attribute: false }) list: () => Promise<VersionRow[]> = async () => [];

    /** Workdir has unsaved changes — surfaces the "Publish first" nudge in the
     * restore confirm (design-log/038 §Q6). */
    @property({ type: Boolean }) dirty = false;

    @state() private _phase: LoadPhase = "loading";
    @state() private _rows: DisplayRow[] = [];
    @state() private _pending: DisplayRow | null = null;

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
        // in (mirrors sync-view, design-log/034).
        void this.#load();
    }

    render() {
        return html`<div class="panel">
            ${this._pending ? this.#renderConfirm(this._pending) : this.#renderList()}
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
        return html`<div class="rows">
            ${this._rows.map((r) => this.#renderRow(r))}
        </div>`;
    }

    #renderRow(r: DisplayRow) {
        // The current version is not a restore target (restoring HEAD is a
        // no-op) — show a badge, no press affordance. Older versions are
        // pressable and open the confirm.
        return html`<rune-row
            ?pressable=${!r.isHead}
            aria-label=${r.isHead ? `${r.dateLabel} (current)` : `Restore ${r.dateLabel}`}
            @press=${r.isHead ? nothing : () => this.#ask(r)}
        >
            <span slot="leading" class="date">${r.dateLabel}</span>
            <span class="meta">${r.metaLabel}</span>
            ${r.isHead
                ? html`<span slot="trailing" class="badge">current</span>`
                : html`<span slot="trailing" class="meta">›</span>`}
        </rune-row>`;
    }

    #renderConfirm(r: DisplayRow) {
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
                <rune-button variant="primary" @press=${this.#confirm}>Restore</rune-button>
            </div>
        `;
    }

    #load = async () => {
        this._pending = null;
        this._phase = "loading";
        try {
            const raw = await this.list();
            this._rows = raw.map((v) => ({
                id: v.id,
                isHead: v.isHead,
                dateLabel: v.unixMs > 0 ? DATE_FMT.format(new Date(v.unixMs)) : v.id,
                metaLabel: `${v.files} ${v.files === 1 ? "file" : "files"} · ${formatBytes(v.sizeBytes)}`,
            }));
            this._phase = "loaded";
        } catch {
            this._phase = "error";
        }
    };

    #ask = (r: DisplayRow) => {
        this._pending = r;
    };

    #cancel = () => {
        this._pending = null;
    };

    #confirm = () => {
        const r = this._pending;
        this._pending = null;
        if (!r) return;
        this.dispatchEvent(
            new CustomEvent<RestoreConfirmDetail>("restore", {
                detail: { refID: r.id },
                bubbles: true,
                composed: true,
            }),
        );
    };

    #publishFirst = () => {
        this._pending = null;
        this.dispatchEvent(new CustomEvent("publishfirst", { bubbles: true, composed: true }));
    };
}

declare global {
    interface HTMLElementTagNameMap {
        "versions-view": VersionsView;
    }
}
