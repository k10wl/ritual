import { css, html, LitElement } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import type { ViewModel } from "../wails-api";
import { retry } from "../wails-api";

@customElement("error-banner")
export class ErrorBanner extends LitElement {
    @property({ attribute: false }) vm!: ViewModel;
    @state() private retrying = false;

    private async onRetry() {
        this.retrying = true;
        try {
            await retry();
        } finally {
            this.retrying = false;
        }
    }

    render() {
        return html`
            <aside class="banner" role="alert">
                <div class="text">
                    <strong>Something went wrong.</strong>
                    <span class="detail">${this.vm.errorText || "Unknown error"}</span>
                </div>
                <button ?disabled=${this.retrying} @click=${this.onRetry}>
                    ${this.retrying ? "Retrying…" : "Retry"}
                </button>
            </aside>
        `;
    }

    static styles = css`
        :host {
            display: block;
            position: sticky;
            top: 0;
            z-index: 20;
        }
        .banner {
            display: flex;
            align-items: center;
            justify-content: space-between;
            gap: var(--space-4);
            padding: var(--space-3) var(--space-4);
            background: color-mix(in srgb, var(--state-fail) 18%, transparent);
            border-bottom: 1px solid color-mix(in srgb, var(--state-fail) 40%, transparent);
            color: var(--text-strong);
        }
        .text {
            display: flex;
            flex-direction: column;
            gap: 2px;
        }
        .detail {
            color: var(--text-muted);
            font-size: var(--fs-2);
            word-break: break-word;
        }
        button {
            padding: var(--space-2) var(--space-3);
            border-radius: var(--radius-md);
            border: 1px solid color-mix(in srgb, var(--state-fail) 50%, transparent);
            background: var(--stone-edge);
            color: var(--text-strong);
            font: inherit;
            cursor: pointer;
        }
        button:disabled {
            opacity: 0.6;
            cursor: progress;
        }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        "error-banner": ErrorBanner;
    }
}
