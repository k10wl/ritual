import { css, html, LitElement } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { ViewModel } from "../wails-api";
import { formatBytes } from "./format";

@customElement("stage-downloading")
export class StageDownloading extends LitElement {
    @property({ attribute: false }) vm!: ViewModel;

    render() {
        const { progress, bytesDone, bytesTotal, label } = this.vm;
        return html`
            <section class="card">
                <div class="spinner" aria-hidden="true"></div>
                <h2>${label || "Downloading…"}</h2>
                <div class="bar" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow=${progress}>
                    <div class="fill" style="width:${progress}%"></div>
                </div>
                <p class="bytes">
                    ${bytesTotal > 0
                        ? html`${formatBytes(bytesDone)} <span class="sep">/</span> ${formatBytes(bytesTotal)}`
                        : html`${progress}%`}
                </p>
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
        }
        .spinner {
            width: 48px;
            height: 48px;
            border-radius: 50%;
            border: 4px solid var(--stone-edge);
            border-top-color: var(--state-idle);
            animation: spin 0.9s linear infinite;
        }
        @keyframes spin {
            to { transform: rotate(360deg); }
        }
        h2 {
            font-size: var(--fs-4);
            margin: 0;
            color: var(--text-strong);
        }
        .bar {
            width: 100%;
            height: 6px;
            border-radius: var(--radius-sm);
            background: var(--stone-edge);
            overflow: hidden;
        }
        .fill {
            height: 100%;
            background: var(--state-idle);
            transition: width 200ms ease;
        }
        .bytes {
            margin: 0;
            color: var(--text-muted);
            font-variant-numeric: tabular-nums;
            font-size: var(--fs-2);
        }
        .sep {
            margin: 0 var(--space-1);
            color: var(--text-faint);
        }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        "stage-downloading": StageDownloading;
    }
}
