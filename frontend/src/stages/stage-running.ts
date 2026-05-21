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
            gap: var(--space-5);
            padding: var(--space-6) var(--space-5);
        }
        .status {
            display: flex;
            align-items: center;
            gap: var(--space-2);
            font-size: var(--fs-4);
        }
        .dot {
            width: 10px;
            height: 10px;
            border-radius: 50%;
            display: inline-block;
        }
        .dot.on {
            background: var(--state-run);
            box-shadow: 0 0 0 3px color-mix(in srgb, var(--state-run) 25%, transparent);
        }
        .dot.starting {
            background: var(--state-prep);
            animation: pulse 1s ease-in-out infinite alternate;
        }
        @keyframes pulse {
            from { opacity: 0.45; }
            to { opacity: 1; }
        }
        .addresses {
            display: flex;
            flex-direction: column;
            gap: var(--space-2);
            background: var(--stone-bevel);
            border-radius: var(--radius-lg);
            padding: var(--space-4);
        }
        .addresses-title {
            font-size: var(--fs-1);
            text-transform: uppercase;
            letter-spacing: 0.08em;
            color: var(--text-faint);
        }
        ul {
            list-style: none;
            margin: 0;
            padding: 0;
            display: flex;
            flex-direction: column;
            gap: var(--space-1);
        }
        li {
            display: grid;
            grid-template-columns: auto 1fr auto;
            align-items: center;
            gap: var(--space-3);
        }
        .lbl {
            color: var(--text-muted);
            font-size: var(--fs-2);
        }
        code {
            font-family: var(--font-mono);
            font-size: var(--fs-3);
            overflow: hidden;
            text-overflow: ellipsis;
        }
        button {
            padding: var(--space-1) var(--space-3);
            border: 1px solid var(--stone-edge);
            border-radius: var(--radius-md);
            background: var(--stone-bevel);
            color: inherit;
            font: inherit;
            cursor: pointer;
        }
        button.danger {
            align-self: flex-end;
            padding: var(--space-2) var(--space-4);
            background: color-mix(in srgb, var(--state-fail) 18%, transparent);
            border: 1px solid color-mix(in srgb, var(--state-fail) 40%, transparent);
            color: var(--text-strong);
        }
        .empty {
            color: var(--text-faint);
        }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        "stage-running": StageRunning;
    }
}
