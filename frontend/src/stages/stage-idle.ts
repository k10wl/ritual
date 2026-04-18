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
            gap: 1rem;
            padding: 2rem 1.5rem;
        }
        h1 {
            font-size: 2.4rem;
            font-weight: 600;
            margin: 0;
            letter-spacing: -0.02em;
        }
        .tagline {
            opacity: 0.7;
            margin: -0.5rem 0 0.5rem;
        }
        .row {
            display: flex;
            align-items: center;
            justify-content: space-between;
            gap: 1rem;
            font-size: 0.95rem;
        }
        .row span {
            opacity: 0.85;
        }
        input {
            width: 120px;
            padding: 0.5rem 0.75rem;
            border: 1px solid rgba(255, 255, 255, 0.15);
            border-radius: 8px;
            background: rgba(255, 255, 255, 0.06);
            color: inherit;
            font-size: 1rem;
            outline: none;
        }
        input:focus {
            border-color: rgba(120, 180, 255, 0.6);
            background: rgba(255, 255, 255, 0.08);
        }
        button.primary {
            margin-top: 0.5rem;
            padding: 0.75rem 1rem;
            border: none;
            border-radius: 10px;
            background: linear-gradient(180deg, #3d82ff, #2563eb);
            color: white;
            font-size: 1.05rem;
            font-weight: 600;
            cursor: pointer;
        }
        button.primary:disabled {
            opacity: 0.6;
            cursor: progress;
        }
        .err {
            color: #ff9e9e;
            font-size: 0.9rem;
            margin: 0;
        }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        "stage-idle": StageIdle;
    }
}
