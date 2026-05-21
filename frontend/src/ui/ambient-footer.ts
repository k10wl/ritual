import { LitElement, css, html } from "lit";
import { customElement } from "lit/decorators.js";

@customElement("ambient-footer")
export class AmbientFooter extends LitElement {
    private emit(action: "logs" | "folder") {
        this.dispatchEvent(new CustomEvent("ambient-action", { detail: action, bubbles: true, composed: true }));
    }

    render() {
        return html`
            <nav>
                <button @click=${() => this.emit("logs")}>log</button>
                <span class="sep" aria-hidden="true">|</span>
                <button @click=${() => this.emit("folder")}>dir</button>
            </nav>
        `;
    }

    static styles = css`
        :host {
            position: fixed;
            left: 0;
            right: 0;
            bottom: 0;
            display: flex;
            justify-content: center;
            padding: var(--space-2) 0 var(--space-1);
            pointer-events: none;
            z-index: 10;
            font-family: var(--font-mono);
            font-size: var(--fs-1);
            letter-spacing: 0.08em;
        }
        nav {
            display: inline-flex;
            align-items: center;
            gap: var(--space-1);
            color: var(--text-faint);
            transition: color 320ms ease;
            pointer-events: auto;
        }
        nav:hover {
            color: var(--text-muted);
        }
        button {
            background: none;
            border: 0;
            padding: 2px;
            margin: 0;
            color: inherit;
            font: inherit;
            letter-spacing: inherit;
            cursor: pointer;
            text-transform: lowercase;
            transition: color 160ms ease;
        }
        button:hover {
            color: var(--text-strong);
        }
        button:focus-visible {
            outline: 1px solid var(--text-muted);
            outline-offset: 2px;
            border-radius: var(--radius-sm);
        }
        .sep {
            color: var(--text-faint);
        }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        "ambient-footer": AmbientFooter;
    }
}
