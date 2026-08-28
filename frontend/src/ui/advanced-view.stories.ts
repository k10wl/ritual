import { html } from "lit";
import "./advanced-view";
import type { SyncVerdict } from "./sync-view";
import type { WorkRootInfo, WorkRootPickResult } from "./work-root";

export default {
    title: "Components / Advanced View",
    component: "advanced-view",
    parameters: {
        docs: {
            description: {
                component:
                    "The single staged Advanced pane (design-log/034): two flat sections — " +
                    "Server (port/memory) and Sync (031). No menu, no nesting. " +
                    "`change` + `sync` bubble to the host.",
            },
        },
    },
};

const pane = (inner: unknown) => html`
    <div style="width:420px; background:var(--stone-deep); border:1px solid var(--stone-bevel);
                border-radius:var(--radius-lg);">
        ${inner}
    </div>
`;

const delayed = (v: SyncVerdict): (() => Promise<SyncVerdict>) =>
    () => new Promise((res) => setTimeout(() => res(v), 400));

const log = (e: Event) =>
    // eslint-disable-next-line no-console
    console.log(e.type, "→", (e as CustomEvent).detail);

const delayedWorkRoot = (v: WorkRootInfo): (() => Promise<WorkRootInfo>) =>
    () => new Promise((res) => setTimeout(() => res(v), 400));
const delayedPick = (v: WorkRootPickResult): (() => Promise<WorkRootPickResult>) =>
    () => new Promise((res) => setTimeout(() => res(v), 400));
const noop = async () => {};

export const Default = () =>
    pane(html`<advanced-view
        .config=${{ port: 25565, memoryMB: 4096 }}
        .check=${delayed({ behind: true, ahead: false })}
        .canUpdate=${true}
        .getWorkRoot=${delayedWorkRoot({ path: "/Users/k10wl/k10wl/ritual", isDefault: true })}
        .openWorkFolder=${noop}
        .pickWorkRootFolder=${delayedPick({ path: "/Volumes/GameDrive/k10wl/ritual", ok: true })}
        .changeWorkRoot=${async (p: string) => log(new CustomEvent("changeworkroot", { detail: p }))}
        @change=${log}
        @sync=${log}
        @checkupdate=${log}
    ></advanced-view>`);

// Updates section gated: when the dial isn't idle the "Check for update" row is
// disabled (design-log/037 §Q4 — the flow restarts the process).
export const UpdateGated = () =>
    pane(html`<advanced-view
        .config=${{ port: 25565, memoryMB: 4096 }}
        .check=${delayed({ behind: false, ahead: false })}
        .canUpdate=${false}
    ></advanced-view>`);
