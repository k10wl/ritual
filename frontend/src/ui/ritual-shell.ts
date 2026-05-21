import { LitElement, css, html } from "lit";
import { customElement } from "lit/decorators.js";
import "./ambient-footer";

@customElement("ritual-shell")
export class RitualShell extends LitElement {
    private relay = (e: Event) => {
        const ce = e as CustomEvent<"logs" | "folder">;
        this.dispatchEvent(new CustomEvent("ambient-action", {
            detail: ce.detail,
            bubbles: true,
            composed: true,
        }));
    };

    render() {
        return html`
            <slot name="banner"></slot>
            <section class="stage">
                <slot></slot>
            </section>
            <ambient-footer @ambient-action=${this.relay}></ambient-footer>
        `;
    }

    static styles = css`
        :host {
            display: flex;
            flex-direction: column;
            min-height: 100vh;
            color: var(--text-strong);
            font-family: var(--font-mono);
            background: radial-gradient(1200px 600px at 20% -10%, var(--rune-soft), transparent 60%),
                radial-gradient(900px 500px at 110% 110%, var(--rune-soft), transparent 65%),
                var(--stone-deep);
        }
        .stage {
            flex: 1;
            display: flex;
            flex-direction: column;
            align-items: center;
            gap: var(--space-5);
            padding: 150px var(--space-4) var(--space-4);
            box-sizing: border-box;
        }
        ::slotted(*) {
            width: 100%;
            max-width: 480px;
        }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        "ritual-shell": RitualShell;
    }
}
