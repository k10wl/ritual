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
            gap: 1rem;
            padding: 0.8rem 1rem;
            background: rgba(255, 70, 70, 0.18);
            border-bottom: 1px solid rgba(255, 70, 70, 0.4);
            color: #ffdede;
        }
        .text {
            display: flex;
            flex-direction: column;
            gap: 0.15rem;
        }
        .detail {
            opacity: 0.8;
            font-size: 0.85rem;
            word-break: break-word;
        }
        button {
            padding: 0.45rem 0.9rem;
            border-radius: 8px;
            border: 1px solid rgba(255, 150, 150, 0.5);
            background: rgba(255, 255, 255, 0.08);
            color: #ffdede;
            font-size: 0.9rem;
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
