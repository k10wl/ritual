import { css, html, LitElement } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import type { ViewModel } from "../wails-api";
import { start } from "../wails-api";

@customElement("stage-idle")
export class StageIdle extends LitElement {
    @property({ attribute: false }) vm!: ViewModel;
    @state() private port = 25565;
    @state() private memoryMB = 4096;
    @state() private busy = false;
    @state() private err = "";

    private async onStart() {
        this.busy = true;
        this.err = "";
        try {
            await start(this.port, this.memoryMB);
        } catch (e) {
            this.err = String((e as Error).message ?? e);
            this.busy = false;
        }
    }

    render() {
        return html`
            <section class="card">
                <h1>Ritual</h1>
                <p class="tagline">Press Start. We'll handle the rest.</p>
                <label class="row">
                    <span>Port</span>
                    <input
                        type="number"
                        min="1"
                        max="65535"
                        .value=${String(this.port)}
                        @input=${(e: InputEvent) => (this.port = Number((e.target as HTMLInputElement).value))}
                    />
                </label>
                <label class="row">
                    <span>Memory (MB)</span>
                    <input
                        type="number"
                        min="512"
                        step="512"
                        .value=${String(this.memoryMB)}
                        @input=${(e: InputEvent) => (this.memoryMB = Number((e.target as HTMLInputElement).value))}
                    />
                </label>
                <button class="primary" ?disabled=${this.busy} @click=${this.onStart}>
                    ${this.busy ? "Starting…" : "Start"}
                </button>
                ${this.err ? html`<p class="err">${this.err}</p>` : ""}
            </section>
        `;
    }

    static styles = css`
        :host {
            display: block;
        }
        .card {
            display: flex;
            flex-direction: column;
            gap: var(--space-4);
            padding: var(--space-6) var(--space-5);
            background: var(--stone-base);
            border: 1px solid var(--stone-edge);
            border-radius: var(--radius-lg);
            box-shadow: 0 1px 0 var(--stone-bevel) inset, 0 8px 24px var(--stone-groove);
        }
        h1 {
            font-size: var(--fs-6);
            margin: 0;
            line-height: var(--lh-tight);
        }
        .tagline {
            color: var(--text-faint);
            margin: calc(-1 * var(--space-2)) 0 var(--space-2);
        }
        .row {
            display: flex;
            align-items: center;
            justify-content: space-between;
            gap: var(--space-4);
            font-size: var(--fs-3);
        }
        .row span {
            color: var(--text-muted);
        }
        input {
            width: 120px;
            padding: var(--space-2) var(--space-3);
            border: 1px solid var(--stone-edge);
            border-radius: var(--radius-md);
            background: var(--stone-bevel);
            color: inherit;
            font: inherit;
            outline: none;
        }
        input:focus {
            border-color: var(--rune-hi);
            background: var(--stone-edge);
        }
        button.primary {
            margin-top: var(--space-2);
            padding: var(--space-3) var(--space-4);
            border: none;
            border-radius: var(--radius-md);
            background: var(--state-idle);
            color: var(--text-strong);
            font: inherit;
            cursor: pointer;
        }
        button.primary:hover:not(:disabled) {
            background: var(--rune);
            color: var(--stone-deep);
        }
        button.primary:disabled {
            opacity: 0.6;
            cursor: progress;
        }
        .err {
            color: var(--state-fail);
            font-size: var(--fs-2);
            margin: 0;
        }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        "stage-idle": StageIdle;
    }
}
