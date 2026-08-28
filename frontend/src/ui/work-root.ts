/**
 * Work root — Advanced section for relocating the content root
 * (design-log/056, Phase F of 055). The current workroot path is itself the
 * "open folder" affordance (click → reveal in the OS file manager); "Change"
 * opens the native OS picker → inline confirm of the chosen path, no modal
 * dialog (house convention, sync-view.ts/versions-view.ts).
 *
 * Presentational: get/open/pick/change are injected by the host (wraps
 * `wails-api`'s GetWorkRoot/OpenRootFolder/PickWorkRootFolder/ChangeWorkRoot).
 * Progress for an in-flight relocate is NOT duplicated here — the main dial
 * already renders it (PhaseRelocating, design-log/055 addendum); this
 * component only tracks its own pending/error state around the calls.
 */

import { LitElement, css, html, nothing } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import "./primitives/rune-button";
import "./primitives/decoder";

export interface WorkRootInfo {
    path: string;
    isDefault: boolean;
}

export interface WorkRootPickResult {
    path: string;
    ok: boolean;
}

type Phase = "loading" | "idle" | "confirm" | "busy" | "error";

@customElement("work-root")
export class WorkRootEl extends LitElement {
    @property({ attribute: false }) get: () => Promise<WorkRootInfo> = async () => ({
        path: "",
        isDefault: true,
    });
    @property({ attribute: false }) open: () => Promise<void> = async () => {};
    @property({ attribute: false }) pick: () => Promise<WorkRootPickResult> = async () => ({
        path: "",
        ok: false,
    });
    @property({ attribute: false }) change: (path: string) => Promise<void> = async () => {};
    // Gates the path click + Change while a session isn't idle (design-log/056
    // §Q4) — mirrors advanced-view's existing `canUpdate` phase gate.
    @property({ type: Boolean }) idle = false;

    @state() private _info: WorkRootInfo | null = null;
    @state() private _phase: Phase = "loading";
    @state() private _pendingPath: string | null = null;
    @state() private _error = "";

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
        .path {
            display: block;
            width: 100%;
            min-height: 1.4em;
            padding: 0;
            border: none;
            background: none;
            color: var(--text-strong);
            font: inherit;
            font-size: var(--fs-body);
            overflow: hidden;
            white-space: nowrap;
            text-overflow: ellipsis;
            cursor: pointer;
        }
        .path:hover:not(:disabled) {
            text-decoration: underline;
        }
        .path:disabled {
            cursor: default;
            color: var(--text-muted);
        }
        .default-badge {
            color: var(--text-faint);
            font-size: var(--fs-caption);
        }
        .confirm-body {
            margin: 0;
            color: var(--text-muted);
            line-height: 1.6;
            word-break: break-all;
        }
        .confirm-actions {
            display: grid;
            grid-template-columns: 1fr 1fr;
            gap: var(--space-3);
        }
        .error {
            margin: 0;
            padding: var(--space-3);
            border-radius: var(--radius-sm);
            color: var(--danger, #e0453a);
            background: color-mix(in srgb, var(--danger, #e0453a) 12%, transparent);
            border: 1px solid color-mix(in srgb, var(--danger, #e0453a) 40%, transparent);
            line-height: 1.5;
        }
    `;

    firstUpdated() {
        void this.#load();
    }

    render() {
        const disabled = !this.idle || this._phase === "loading" || this._phase === "busy";
        return html`<div class="panel">
            <button type="button" class="path" ?disabled=${disabled} @click=${this.#openFolder}>
                <rune-decoder .text=${this.#pathText()}></rune-decoder>
            </button>
            ${this._info?.isDefault ? html`<span class="default-badge">Default location</span>` : nothing}
            ${this._error ? html`<p class="error">${this._error}</p>` : nothing}
            ${this._phase === "confirm"
                ? this.#renderConfirm()
                : html`<rune-button variant="tinted" size="sm" ?disabled=${disabled} @press=${this.#pickFolder}
                      >Change</rune-button
                  >`}
        </div>`;
    }

    #pathText(): string {
        if (this._phase === "loading") return "Loading…";
        return this._info?.path ?? "";
    }

    #renderConfirm() {
        return html`
            <p class="confirm-body">Move all game data to<br /><strong>${this._pendingPath}</strong>?</p>
            <div class="confirm-actions">
                <rune-button variant="tinted" @press=${this.#cancel}>Cancel</rune-button>
                <rune-button variant="primary" @press=${this.#confirmChange}>Change</rune-button>
            </div>
        `;
    }

    async #load() {
        this._phase = "loading";
        this._info = await this.get();
        this._phase = "idle";
    }

    #openFolder = () => {
        void this.open();
    };

    #pickFolder = async () => {
        this._error = "";
        const result = await this.pick();
        if (!result.ok) return;
        this._pendingPath = result.path;
        this._phase = "confirm";
    };

    #cancel = () => {
        this._pendingPath = null;
        this._phase = "idle";
    };

    #confirmChange = async () => {
        const path = this._pendingPath;
        if (!path) return;
        this._pendingPath = null;
        this._phase = "busy";
        try {
            await this.change(path);
            await this.#load();
        } catch (e) {
            this._error = e instanceof Error ? e.message : "Couldn't change the work folder.";
            this._phase = "error";
        }
    };
}

declare global {
    interface HTMLElementTagNameMap {
        "work-root": WorkRootEl;
    }
}
