import { html } from "lit";
import "./work-root";
import type { WorkRootInfo, WorkRootPickResult } from "./work-root";

export default {
    title: "Components / Work Root",
    component: "work-root",
    parameters: {
        docs: {
            description: {
                component:
                    "Advanced section for relocating the content root (design-log/056, Phase F of 055). " +
                    "The path itself is clickable (reveals in the OS file manager); Change opens the " +
                    "native OS folder picker → inline confirm of the chosen path → `ChangeWorkRoot`. " +
                    "No modal dialog; progress for the move itself renders on the main dial " +
                    "(PhaseRelocating), not duplicated here.",
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

const delayed = <T,>(v: T, ms = 300): (() => Promise<T>) => () => new Promise((res) => setTimeout(() => res(v), ms));

const log = (e: Event) =>
    // eslint-disable-next-line no-console
    console.log(e.type, "→", (e as CustomEvent).detail ?? "(no detail)");

const noop = async () => {};

// Non-default location, idle session.
export const Default = () =>
    pane(html`<work-root
        .get=${delayed<WorkRootInfo>({ path: "/Volumes/GameDrive/k10wl/ritual", isDefault: false })}
        .open=${noop}
        .pick=${delayed<WorkRootPickResult>({ path: "/Volumes/GameDrive/k10wl/ritual", ok: true })}
        .change=${async (p: string) => log(new CustomEvent("change", { detail: p }))}
        idle
    ></work-root>`);

// Still at the default install location.
export const AtDefaultLocation = () =>
    pane(html`<work-root
        .get=${delayed<WorkRootInfo>({ path: "/Users/k10wl/k10wl/ritual", isDefault: true })}
        .open=${noop}
        .pick=${delayed<WorkRootPickResult>({ path: "", ok: false })}
        .change=${noop}
        idle
    ></work-root>`);

// User pressed "Change" and picked a destination — inline confirm before
// anything moves (design-log/056 §Q2).
export const ConfirmPending = () => {
    const el = document.createElement("work-root");
    el.get = delayed<WorkRootInfo>({ path: "/Users/k10wl/k10wl/ritual", isDefault: true });
    el.open = noop;
    el.pick = async () => ({ path: "/Volumes/GameDrive/k10wl/ritual", ok: true });
    el.change = noop;
    el.idle = true;
    queueMicrotask(() => el.shadowRoot?.querySelector("rune-button")?.dispatchEvent(new Event("press")));
    return pane(html`${el}`);
};

// A rejected/failed ChangeWorkRoot (validation, permissions, running session,
// corrupted-blob abort) — inline error text, no partial state.
export const ErrorState = () =>
    pane(html`<work-root
        .get=${delayed<WorkRootInfo>({ path: "/Users/k10wl/k10wl/ritual", isDefault: true })}
        .open=${noop}
        .pick=${delayed<WorkRootPickResult>({ path: "/Volumes/GameDrive/k10wl/ritual", ok: true })}
        .change=${async () => {
            throw new Error("A session is currently running.");
        }}
        idle
    ></work-root>`);

// Session not idle (server running / another relocate in flight) — path click
// and Change both gated off, same treatment as "Check for update" (037 §Q4).
export const NotIdle = () =>
    pane(html`<work-root
        .get=${delayed<WorkRootInfo>({ path: "/Volumes/GameDrive/k10wl/ritual", isDefault: false })}
        .open=${noop}
        .pick=${noop}
        .change=${noop}
    ></work-root>`);
