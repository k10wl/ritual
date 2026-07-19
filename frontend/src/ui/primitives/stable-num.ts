import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";

export type StableNumAlign = "left" | "center" | "right";

@customElement("stable-num")
export class StableNum extends LitElement {
    @property({ type: Number }) chars = 6;
    @property({ reflect: true }) align: StableNumAlign = "right";

    willUpdate(changed: Map<string, unknown>) {
        if (changed.has("chars")) {
            this.style.setProperty("min-width", `${this.chars}ch`);
        }
    }

    render() {
        return html`<slot></slot>`;
    }

    static styles = css`
        :host {
            display: inline-block;
            font-variant-numeric: tabular-nums;
            white-space: nowrap;
        }
        :host([align="right"])  { text-align: right; }
        :host([align="center"]) { text-align: center; }
        :host([align="left"])   { text-align: left; }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        "stable-num": StableNum;
    }
}
