import { css, html, LitElement } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import type { ViewModel } from "../wails-api";
import { retry } from "../wails-api";

@customElement("stage-locked")
export class StageLocked extends LitElement {
    @property({ attribute: false }) vm!: ViewModel;
    @state() private checking = false;

    private async onRetry() {
        this.checking = true;
        try {
            await retry();
        } catch {
            // stage will flip back to idle/locked based on projection output
        } finally {
            this.checking = false;
        }
    }

    render() {
        const holder = this.vm.lockHolder || "Someone";
        return html`
            <section class="card">
                <div class="icon" aria-hidden="true">🎮</div>
                <h2>${holder} is playing</h2>
                <p class="sub">You'll get a turn when they finish.</p>
                <button ?disabled=${this.checking} @click=${this.onRetry}>
                    ${this.checking ? "Checking…" : "Check again"}
                </button>
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
            align-items: center;
            gap: 1rem;
            padding: 3rem 1.5rem;
            text-align: center;
        }
        .icon {
            font-size: 3.5rem;
        }
        h2 {
            font-size: 1.4rem;
            margin: 0;
            font-weight: 600;
        }
        .sub {
            margin: 0;
            opacity: 0.7;
            max-width: 26ch;
        }
        button {
            margin-top: 0.6rem;
            padding: 0.7rem 1.3rem;
            border: 1px solid rgba(255, 255, 255, 0.2);
            border-radius: 10px;
            background: rgba(255, 255, 255, 0.05);
            color: inherit;
            font-size: 0.95rem;
            cursor: pointer;
        }
        button:disabled {
            opacity: 0.55;
            cursor: progress;
        }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        "stage-locked": StageLocked;
    }
}
