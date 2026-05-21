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
            gap: var(--space-4);
            padding: var(--space-7) var(--space-5);
            text-align: center;
        }
        .icon {
            font-size: 56px;
        }
        h2 {
            font-size: var(--fs-5);
            margin: 0;
            color: var(--text-strong);
        }
        .sub {
            margin: 0;
            color: var(--text-muted);
            max-width: 26ch;
        }
        button {
            margin-top: var(--space-2);
            padding: var(--space-3) var(--space-4);
            border: 1px solid var(--stone-edge);
            border-radius: var(--radius-md);
            background: var(--stone-bevel);
            color: inherit;
            font: inherit;
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
