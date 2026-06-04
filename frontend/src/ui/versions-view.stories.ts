import { html } from "lit";
import "./versions-view";
import type { VersionRow } from "./versions-view";

export default {
    title: "Components / Versions View",
    component: "versions-view",
    parameters: {
        docs: {
            description: {
                component:
                    "World-save rollback (design-log/038) as a section of Advanced. Lists historical " +
                    "refs newest-first; tapping an older one reveals an inline two-step confirm (no " +
                    "dialog/popup/toast). The listing is injected via `.list`; confirming emits " +
                    "`restore` {refID}. `dirty` adds a non-blocking 'Publish first' nudge.",
            },
        },
    },
};

const pane = (inner: unknown) => html`
    <div style="width:420px; background:var(--stone-deep); border:1px solid var(--stone-bevel);
                border-radius:var(--radius-lg); padding:var(--space-4);">
        ${inner}
    </div>
`;

// A synthetic history: HEAD plus three older snapshots of varying size.
const SAMPLE: VersionRow[] = [
    { id: "2026-06-04T09-30-00.000Z", unixMs: Date.UTC(2026, 5, 4, 9, 30), files: 128, sizeBytes: 734_003_200, isHead: true, source: "remote" },
    { id: "2026-06-03T21-05-00.000Z", unixMs: Date.UTC(2026, 5, 3, 21, 5), files: 126, sizeBytes: 720_000_000, isHead: false, source: "remote" },
    { id: "2026-05-28T18-40-00.000Z", unixMs: Date.UTC(2026, 4, 28, 18, 40), files: 120, sizeBytes: 690_000_000, isHead: false, source: "remote" },
    { id: "2026-05-12T12-00-00.000Z", unixMs: Date.UTC(2026, 4, 12, 12, 0), files: 1, sizeBytes: 4096, isHead: false, source: "remote" },
];

const delayed = (v: VersionRow[]): (() => Promise<VersionRow[]>) =>
    () => new Promise((res) => setTimeout(() => res(v), 400));

const log = (e: Event) =>
    // eslint-disable-next-line no-console
    console.log(e.type, "→", (e as CustomEvent).detail);

export const History = () =>
    pane(html`<versions-view .list=${delayed(SAMPLE)} @restore=${log}></versions-view>`);

// Dirty workdir → the restore confirm surfaces the "Publish first" nudge.
export const Dirty = () =>
    pane(html`<versions-view dirty .list=${delayed(SAMPLE)} @restore=${log} @publishfirst=${log}></versions-view>`);

// Only the current version exists → nothing to roll back to.
export const OnlyCurrent = () =>
    pane(html`<versions-view .list=${delayed([SAMPLE[0]])} @restore=${log}></versions-view>`);

// Fresh install / empty history.
export const Empty = () => pane(html`<versions-view .list=${delayed([])} @restore=${log}></versions-view>`);

// Listing rejects → graceful "couldn't load" with retry.
export const Error = () =>
    pane(html`<versions-view
        .list=${() => Promise.reject(new globalThis.Error("offline"))}
        @restore=${log}
    ></versions-view>`);
