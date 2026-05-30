import { html } from "lit";
import "./advanced-view";
import type { SyncVerdict } from "./sync-view";

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

export const Default = () =>
    pane(html`<advanced-view
        .config=${{ port: 25565, memoryMB: 4096 }}
        .check=${delayed({ behind: true, ahead: false })}
        @change=${log}
        @sync=${log}
    ></advanced-view>`);
