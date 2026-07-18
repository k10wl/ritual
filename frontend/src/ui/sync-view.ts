/**
 * Sync view — the server-free Download / Upload gestures (design-log/031) as a
 * pushed tenant of the navigation stack (design-log/034). An explicit **Check**
 * resolves whether the remote has a newer world to pull, or the local has
 * changes to publish, then offers exactly the applicable direction in plain
 * language. The destructive confirm is an **inline two-step reveal** — no
 * dialog, no popup, no toast (the user's brief).
 *
 * Presentational: the HEAD probe is injected via `.check` (the host wraps
 * `getSyncStatus`); confirming a direction emits `sync` for the host to run and
 * to unwind the stack. Lives in the component layer but stays free of
 * `wails-api` so it is testable and Storybook-able across every verdict.
 *
 * HIG — replace modal confirmation with progressive, in-context disclosure:
 * https://developer.apple.com/design/human-interface-guidelines/navigation-and-search
 */

import { LitElement, css, html, nothing } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import "./primitives/rune-button";
import "./primitives/decoder";

export type SyncDirection = "download" | "upload" | "revert";

/**
 * What a check found. Unlike 031's exclusive trichotomy, 035 lets `behind` and
 * `ahead` co-occur: `ahead` keys on any uncanonical local state (`dirty ||
 * unpushed`), so the remote can be newer (`behind`) while local also has work to
 * publish. Both-true renders Publish primary + a loud "remote is newer" warning.
 *
 * `dirty` is needed too for the Revert confirm copy (design-log/045 §C / §Q5):
 * the confirm message adapts depending on whether the user actually has
 * uncommitted edits or only an unpushed commit (Revert is an observable no-op
 * in that case but we still surface the button per user direction).
 */
export interface SyncVerdict {
    behind: boolean; // remote HEAD newer than local → something to Download
    ahead: boolean; // local has uncanonical work (dirty || unpushed) → Publish
    dirty?: boolean; // workdir ≠ HEAD (subset of ahead) → Revert has visible effect
}

export interface SyncConfirmDetail {
    direction: SyncDirection;
}

type CheckPhase = "unchecked" | "checking" | "checked" | "error";

// Humane confirm copy (design-log/031 §Q8 spirit): the action is
// destructive-in-spirit, so Cancel is the calm default and the body spells out
// the consequence — but presented inline, never as a dialog.
const CONFIRM_COPY: Record<Exclude<SyncDirection, "revert">, { body: string; cta: string }> = {
    download: {
        body: "Get the latest world from the remote. This replaces your local copy and removes local-only files in the synced folder.",
        cta: "Download",
    },
    upload: {
        // Data-sacred framing (design-log/035 §Q4c / OQ2): publishing creates a
        // version, it never destroys — nothing is lost.
        body: "Publish your local worlds as the version everyone gets. Your current state becomes the latest — nothing is lost.",
        cta: "Publish",
    },
};

// Revert copy adapts to dirty vs unpushed-only (design-log/045 §Q5). The dirty
// case is the headline action — drops uncommitted edits. The unpushed-only
// case is honest about being a no-op so the user isn't misled into thinking
// the button will drop their unpushed commit.
const REVERT_COPY = {
    dirty: {
        body: "Throw away your unsaved changes and bring back the last saved version. Nothing on the remote changes.",
        cta: "Revert",
    },
    unpushedOnly: {
        body: "Re-apply the last saved version to your workdir. Nothing changes — there's nothing to throw away.",
        cta: "Revert",
    },
};

@customElement("sync-view")
export class SyncView extends LitElement {
    /** HEAD probe, injected by the host (wraps `getSyncStatus`). */
    @property({ attribute: false }) check: () => Promise<SyncVerdict> = async () => ({
        behind: false,
        ahead: false,
    });

    /**
     * Run the check automatically on first render. The Advanced pane is
     * lazily (re)mounted on every navigation into it (design-log/034), so this
     * re-probes the HEAD on each transition — the verdict is never stale, and
     * the glitch-decode plays as the pane slides in. Manual "Check again" stays
     * for re-runs without leaving.
     */
    @property({ type: Boolean, reflect: true }) auto = false;

    @state() private _phase: CheckPhase = "unchecked";
    @state() private _verdict: SyncVerdict = { behind: false, ahead: false };
    @state() private _pending: SyncDirection | null = null;

    static styles = css`
        :host {
            display: block;
            font-family: var(--font-mono);
            color: var(--text);
        }
        .panel {
            padding: var(--space-5) var(--space-4);
            display: flex;
            flex-direction: column;
            gap: var(--space-4);
        }
        .hint {
            margin: 0;
            color: var(--text-muted);
            line-height: 1.6;
        }
        /* Verdict line decodes (glitches) on every state change instead of a
           plain text jump — design-log/008 rune-decoder. */
        .status {
            display: block;
            min-height: 1.4em;
            color: var(--text-strong);
            font-size: var(--fs-body);
        }
        .confirm-body {
            margin: 0;
            color: var(--text-muted);
            line-height: 1.6;
        }
        .confirm-actions {
            display: grid;
            grid-template-columns: 1fr 1fr;
            gap: var(--space-3);
        }
        /* Loud "remote is newer" warning shown alongside Publish when behind &&
           ahead — a prominent treatment (warning color/weight), NOT the muted
           echo of the passive cue. The whole intervention is the warning text;
           publishing is never blocked (design-log/035 §Q4c). */
        .warn {
            margin: 0;
            padding: var(--space-3);
            border-radius: var(--radius-sm);
            color: var(--warning, #e0a106);
            background: color-mix(in srgb, var(--warning, #e0a106) 12%, transparent);
            border: 1px solid color-mix(in srgb, var(--warning, #e0a106) 40%, transparent);
            font-weight: 600;
            line-height: 1.5;
        }
        .actions {
            display: flex;
            flex-direction: column;
            gap: var(--space-3);
        }
    `;

    firstUpdated() {
        // Re-probe on entry (lazy remount = one run per Advanced transition).
        if (this.auto) void this.#runCheck();
    }

    render() {
        return html`<div class="panel">
            ${this._pending ? this.#renderConfirm(this._pending) : this.#renderStatus()}
        </div>`;
    }

    // The verdict line is ONE persistent <rune-decoder>: feeding it new text on
    // each state change makes the words glitch-decode in place rather than
    // hard-swap. Empty while unchecked (the hint carries the prompt instead).
    #renderStatus() {
        return html`
            <rune-decoder class="status" .text=${this.#statusText()}></rune-decoder>
            ${this._phase === "unchecked"
                ? html`<p class="hint">
                      See whether the remote has a newer world to download, or whether your
                      local changes are ready to publish.
                  </p>`
                : nothing}
            ${this.#renderActions()}
        `;
    }

    #statusText(): string {
        switch (this._phase) {
            case "checking":
                return "Checking…";
            case "error":
                return "Couldn't reach the remote.";
            case "checked": {
                const { behind, ahead } = this._verdict;
                // ahead takes precedence: local work to publish is the headline;
                // the behind case adds a loud warning beside Publish, not a
                // status swap (design-log/035 §Q4c).
                if (ahead) return "You have local changes to publish.";
                if (behind) return "A newer world is waiting.";
                return "Everything's up to date.";
            }
            default:
                return "";
        }
    }

    #renderActions() {
        switch (this._phase) {
            case "checking":
                return nothing;
            case "error":
                return html`<rune-button variant="tinted" @press=${this.#runCheck}>Try again</rune-button>`;
            case "checked": {
                const { behind, ahead } = this._verdict;
                // Revert appears whenever publishable (design-log/045 §Q5
                // decided: dirty || unpushed). Secondary action — Publish stays
                // primary. In the unpushed-only case Revert is observably a
                // no-op; the confirm copy is honest about it (REVERT_COPY).
                const revertBtn = ahead
                    ? html`<rune-button variant="tinted" @press=${this.#ask("revert")}>Revert to last saved</rune-button>`
                    : nothing;
                // Both-true: Publish is primary, with a loud warning + a
                // secondary "Download latest". Non-blocking (design-log/035
                // §Q4b/c) — the warning is the whole intervention.
                if (ahead && behind)
                    return html`
                        <p class="warn">
                            The remote is newer. Publishing makes your version the latest and
                            buries the newer one.
                        </p>
                        <div class="actions">
                            <rune-button variant="primary" @press=${this.#ask("upload")}>Publish</rune-button>
                            <rune-button variant="tinted" @press=${this.#ask("download")}>Download latest</rune-button>
                            ${revertBtn}
                        </div>
                    `;
                if (ahead)
                    return html`<div class="actions">
                        <rune-button variant="primary" @press=${this.#ask("upload")}>Publish</rune-button>
                        ${revertBtn}
                    </div>`;
                if (behind)
                    return html`<rune-button variant="primary" @press=${this.#ask("download")}>⬇ Download</rune-button>`;
                return html`<rune-button variant="plain" size="sm" @press=${this.#runCheck}>Check again</rune-button>`;
            }
            default:
                return html`<rune-button variant="primary" @press=${this.#runCheck}>Check remote</rune-button>`;
        }
    }

    #renderConfirm(direction: SyncDirection) {
        // Revert has its own copy table that depends on whether dirty is true
        // (design-log/045 §Q5). Other directions read from CONFIRM_COPY.
        if (direction === "revert") {
            const copy = this._verdict.dirty ? REVERT_COPY.dirty : REVERT_COPY.unpushedOnly;
            return html`
                <p class="confirm-body">${copy.body}</p>
                <div class="confirm-actions">
                    <rune-button variant="tinted" @press=${this.#cancel}>Cancel</rune-button>
                    <rune-button variant="primary" @press=${this.#confirm}>${copy.cta}</rune-button>
                </div>
            `;
        }
        const copy = CONFIRM_COPY[direction];
        return html`
            <p class="confirm-body">${copy.body}</p>
            <div class="confirm-actions">
                <rune-button variant="tinted" @press=${this.#cancel}>Cancel</rune-button>
                <rune-button variant="primary" @press=${this.#confirm}>${copy.cta}</rune-button>
            </div>
        `;
    }

    #runCheck = async () => {
        this._pending = null;
        this._phase = "checking";
        try {
            this._verdict = await this.check();
            this._phase = "checked";
        } catch {
            this._phase = "error";
        }
    };

    #ask = (direction: SyncDirection) => () => {
        this._pending = direction;
    };

    #cancel = () => {
        this._pending = null;
    };

    #confirm = () => {
        const direction = this._pending;
        this._pending = null;
        if (!direction) return;
        this.dispatchEvent(
            new CustomEvent<SyncConfirmDetail>("sync", {
                detail: { direction },
                bubbles: true,
                composed: true,
            }),
        );
    };
}

declare global {
    interface HTMLElementTagNameMap {
        "sync-view": SyncView;
    }
}
