import { css, html, LitElement } from "lit";
import { customElement, state } from "lit/decorators.js";
import { getSnapshot, onView, openRootFolder, showLogs, Stage, ViewModel } from "./wails-api";

import "./stages/stage-idle";
import "./stages/stage-downloading";
import "./stages/stage-running";
import "./stages/stage-uploading";
import "./stages/stage-locked";
import "./stages/error-banner";

const FALLBACK_VM: ViewModel = new ViewModel({ stage: Stage.StageIdle });

@customElement("ritual-app")
export class RitualApp extends LitElement {
    @state() private vm: ViewModel = FALLBACK_VM;
    private unsubscribe?: () => void;

    async connectedCallback() {
        super.connectedCallback();
        try {
            this.vm = await getSnapshot();
        } catch {
            // first render relies on FALLBACK_VM until the first Emit arrives
        }
        this.unsubscribe = onView((vm) => {
            this.vm = vm;
        });
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        this.unsubscribe?.();
    }

    private stageBody() {
        switch (this.vm.stage) {
            case Stage.StageDownloading:
                return html`<stage-downloading .vm=${this.vm}></stage-downloading>`;
            case Stage.StageRunning:
                return html`<stage-running .vm=${this.vm}></stage-running>`;
            case Stage.StageUploading:
                return html`<stage-uploading .vm=${this.vm}></stage-uploading>`;
            case Stage.StageLocked:
                return html`<stage-locked .vm=${this.vm}></stage-locked>`;
            case Stage.StageFailed:
            case Stage.StageIdle:
            default:
                return html`<stage-idle .vm=${this.vm}></stage-idle>`;
        }
    }

    render() {
        const showBanner = this.vm.stage === Stage.StageFailed && this.vm.errorText !== "";
        return html`
            ${showBanner ? html`<error-banner .vm=${this.vm}></error-banner>` : ""}
            <header class="chrome">
                <button class="chip" @click=${() => openRootFolder()} title="Open Ritual folder in file manager">Folder</button>
                <button class="chip" @click=${() => showLogs()} title="Open log console">Logs</button>
            </header>
            <main>${this.stageBody()}</main>
        `;
    }

    static styles = css`
        :host {
            display: flex;
            flex-direction: column;
            min-height: 100vh;
            color: #f4f4f6;
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Inter", sans-serif;
            background: radial-gradient(1200px 600px at 20% -10%, rgba(70, 110, 200, 0.25), transparent 60%),
                radial-gradient(900px 500px at 110% 110%, rgba(180, 80, 150, 0.18), transparent 60%),
                #0f131a;
        }
        .chrome {
            display: flex;
            justify-content: flex-end;
            padding: 0.5rem 0.75rem;
        }
        .chrome {
            gap: 0.4rem;
        }
        .chrome .chip {
            padding: 0.3rem 0.8rem;
            background: rgba(255, 255, 255, 0.05);
            border: 1px solid rgba(255, 255, 255, 0.1);
            color: rgba(255, 255, 255, 0.75);
            border-radius: 8px;
            font-size: 0.8rem;
            cursor: pointer;
        }
        .chrome .chip:hover {
            background: rgba(255, 255, 255, 0.1);
            color: #fff;
        }
        main {
            flex: 1;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 1rem;
        }
        main > * {
            width: 100%;
            max-width: 480px;
        }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        "ritual-app": RitualApp;
    }
}
