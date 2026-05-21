import { css, html, LitElement } from "lit";
import { customElement, state } from "lit/decorators.js";
import { getSnapshot, onView, openRootFolder, showLogs, Stage, ViewModel } from "./wails-api";
import type { DialState } from "./ui/ritual-dial";
import "./ui/ritual-shell";

const stageToDial = (s: Stage): DialState => {
    switch (s) {
        case Stage.StageDownloading: return "prep";
        case Stage.StageRunning:     return "run";
        case Stage.StageUploading:   return "final";
        case Stage.StageFailed:      return "fail";
        default:                     return "idle";
    }
};

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
            <ritual-shell
                .state=${stageToDial(this.vm.stage)}
                @ambient-action=${(e: CustomEvent<"logs" | "folder">) =>
                    e.detail === "logs" ? showLogs() : openRootFolder()}
            >
                ${showBanner
                    ? html`<error-banner slot="banner" .vm=${this.vm}></error-banner>`
                    : ""}
                ${this.stageBody()}
            </ritual-shell>
        `;
    }

    static styles = css`
        :host { display: contents; }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        "ritual-app": RitualApp;
    }
}
