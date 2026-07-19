import { html } from "lit";
import "./sync-view";
import type { SyncVerdict } from "./sync-view";

export default {
    title: "Components / Sync View",
    component: "sync-view",
    parameters: {
        docs: {
            description: {
                component:
                    "Server-free sync (design-log/031) as a navigation-stack tenant (034). " +
                    "Explicit Check → humane verdict → inline two-step confirm. No dialog/popup/toast. " +
                    "The HEAD probe is injected via `.check`; confirming emits `sync` {direction}.",
            },
        },
    },
};

// A fixed pane approximating a pushed view inside the 560-wide window.
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
    console.log("sync →", (e as CustomEvent).detail);

// Remote moved ahead → Download offered.
export const Behind = () =>
    pane(html`<sync-view .check=${delayed({ behind: true, ahead: false })} @sync=${log}></sync-view>`);

// Local moved ahead → Upload offered.
export const Ahead = () =>
    pane(html`<sync-view .check=${delayed({ behind: false, ahead: true })} @sync=${log}></sync-view>`);

// Heads equal → nothing to do.
export const InSync = () =>
    pane(html`<sync-view .check=${delayed({ behind: false, ahead: false })} @sync=${log}></sync-view>`);

// Probe rejects → graceful "couldn't reach" with retry.
export const Offline = () =>
    pane(html`<sync-view
        .check=${() => Promise.reject(new Error("offline"))}
        @sync=${log}
    ></sync-view>`);
