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

export type SyncDirection = "download" | "upload";

/** What a check found: at most one is true under the HEAD-timestamp trichotomy. */
export interface SyncVerdict {
    behind: boolean; // remote HEAD newer than local → something to Download
    ahead: boolean; // local HEAD newer than remote → something to Upload
}

export interface SyncConfirmDetail {
    direction: SyncDirection;
}

type CheckPhase = "unchecked" | "checking" | "checked" | "error";

// Humane confirm copy (design-log/031 §Q8 spirit): the action is
// destructive-in-spirit, so Cancel is the calm default and the body spells out
// the consequence — but presented inline, never as a dialog.
const CONFIRM_COPY: Record<SyncDirection, { body: string; cta: string }> = {
    download: {
        body: "Get the latest world from the remote. This replaces your local copy and removes local-only files in the synced folder.",
        cta: "Download",
    },
    upload: {
        body: "Publish your local worlds as a new remote version. This can't be undone from inside the app.",
        cta: "Upload",
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
                if (behind) return "A newer world is waiting.";
                if (ahead) return "You have local changes to publish.";
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
                if (behind)
                    return html`<rune-button variant="primary" @press=${this.#ask("download")}>⬇ Download</rune-button>`;
                if (ahead)
                    return html`<rune-button variant="primary" @press=${this.#ask("upload")}>⬆ Upload</rune-button>`;
                return html`<rune-button variant="plain" size="sm" @press=${this.#runCheck}>Check again</rune-button>`;
            }
            default:
                return html`<rune-button variant="primary" @press=${this.#runCheck}>Check remote</rune-button>`;
        }
    }

    #renderConfirm(direction: SyncDirection) {
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
