import { css, html, LitElement } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { ViewModel } from "../wails-api";
import { formatBytes } from "./format";

@customElement("stage-uploading")
export class StageUploading extends LitElement {
    @property({ attribute: false }) vm!: ViewModel;

    render() {
        const { progress, bytesDone, bytesTotal, label } = this.vm;
        return html`
            <section class="card">
                <div class="spinner" aria-hidden="true"></div>
                <h2>${label || "Saving…"}</h2>
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
            gap: 1rem;
            padding: 3rem 1.5rem;
        }
        .spinner {
            width: 48px;
            height: 48px;
            border-radius: 50%;
            border: 4px solid rgba(255, 255, 255, 0.1);
            border-top-color: #ff9e3d;
            animation: spin 0.9s linear infinite;
        }
        @keyframes spin {
            to { transform: rotate(360deg); }
        }
        h2 {
            font-size: 1.2rem;
            font-weight: 500;
            margin: 0;
            opacity: 0.9;
        }
        .bar {
            width: 100%;
            height: 6px;
            border-radius: 3px;
            background: rgba(255, 255, 255, 0.08);
            overflow: hidden;
        }
        .fill {
            height: 100%;
            background: linear-gradient(90deg, #ff9e3d, #ffc876);
            transition: width 200ms ease;
        }
        .bytes {
            margin: 0;
            opacity: 0.7;
            font-variant-numeric: tabular-nums;
            font-size: 0.9rem;
        }
        .sep {
            margin: 0 0.35rem;
            opacity: 0.4;
        }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        "stage-uploading": StageUploading;
    }
}
