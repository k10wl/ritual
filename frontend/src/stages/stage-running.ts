import { css, html, LitElement } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import type { ViewModel } from "../wails-api";
import { stop } from "../wails-api";

@customElement("stage-running")
export class StageRunning extends LitElement {
    @property({ attribute: false }) vm!: ViewModel;
    @state() private copied = "";
    @state() private stopping = false;

    private async copy(addr: string) {
        try {
            await navigator.clipboard.writeText(addr);
            this.copied = addr;
            setTimeout(() => {
                if (this.copied === addr) this.copied = "";
            }, 1400);
        } catch {
            this.copied = "";
        }
    }

    private async onStop() {
        this.stopping = true;
        try {
            await stop();
        } catch {
            this.stopping = false;
        }
    }

    render() {
        const { readyLight, label, addresses } = this.vm;
        return html`
            <section class="card">
                <header class="status">
                    <span class=${"dot " + (readyLight ? "on" : "starting")}></span>
                    <span class="label">${label || (readyLight ? "Ready" : "Starting…")}</span>
                </header>
                <div class="addresses">
                    <div class="addresses-title">Share to join</div>
                    ${addresses.length === 0
                        ? html`<div class="empty">—</div>`
                        : html`<ul>
                              ${addresses.map(
                                  (a) => html`
                                      <li>
                                          <span class="lbl">${a.label}</span>
                                          <code>${a.address}</code>
                                          <button @click=${() => this.copy(a.address)}>
                                              ${this.copied === a.address ? "Copied" : "Copy"}
                                          </button>
                                      </li>
                                  `,
                              )}
                          </ul>`}
                </div>
                <button class="danger" ?disabled=${this.stopping} @click=${this.onStop}>
                    ${this.stopping ? "Stopping…" : "Stop"}
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
            gap: 1.25rem;
            padding: 2rem 1.5rem;
        }
        .status {
            display: flex;
            align-items: center;
            gap: 0.6rem;
            font-size: 1.1rem;
        }
        .dot {
            width: 0.65rem;
            height: 0.65rem;
            border-radius: 50%;
            display: inline-block;
        }
        .dot.on {
            background: #51cf66;
            box-shadow: 0 0 0 3px rgba(81, 207, 102, 0.25);
        }
        .dot.starting {
            background: #f9c74f;
            animation: pulse 1s ease-in-out infinite alternate;
        }
        @keyframes pulse {
            from { opacity: 0.45; }
            to { opacity: 1; }
        }
        .addresses {
            display: flex;
            flex-direction: column;
            gap: 0.5rem;
            background: rgba(255, 255, 255, 0.04);
            border-radius: 12px;
            padding: 1rem;
        }
        .addresses-title {
            font-size: 0.8rem;
            text-transform: uppercase;
            letter-spacing: 0.08em;
            opacity: 0.6;
        }
        ul {
            list-style: none;
            margin: 0;
            padding: 0;
            display: flex;
            flex-direction: column;
            gap: 0.4rem;
        }
        li {
            display: grid;
            grid-template-columns: auto 1fr auto;
            align-items: center;
            gap: 0.75rem;
        }
        .lbl {
            opacity: 0.75;
            font-size: 0.9rem;
        }
        code {
            font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
            font-size: 0.95rem;
            overflow: hidden;
            text-overflow: ellipsis;
        }
        button {
            padding: 0.4rem 0.8rem;
            border: 1px solid rgba(255, 255, 255, 0.14);
            border-radius: 8px;
            background: rgba(255, 255, 255, 0.06);
            color: inherit;
            font-size: 0.85rem;
            cursor: pointer;
        }
        button.danger {
            align-self: flex-end;
            padding: 0.6rem 1.1rem;
            background: rgba(255, 110, 110, 0.18);
            border: 1px solid rgba(255, 110, 110, 0.4);
            color: #ff9e9e;
            font-weight: 600;
        }
        .empty {
            opacity: 0.5;
        }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        "stage-running": StageRunning;
    }
}
