import { html } from "lit";
import "./rune-segmented";
import type { SegmentOption } from "./rune-segmented";

export default {
    title: "Primitives / Rune Segmented",
    component: "rune-segmented",
    parameters: {
        docs: {
            description: {
                component:
                    "Mutually-exclusive pick over a small set, shown all at once (design-log/033 §Q1). " +
                    "role=radiogroup + roving tabindex (←/→/Home/End). Emits `change` {value}. " +
                    "Drives the retention tier picker (0–5) and the Local·R2 scope switch.",
            },
        },
    },
};

const tiers: SegmentOption[] = ["0", "1", "2", "3", "4", "5"].map((v) => ({ value: v, label: v }));
const scope: SegmentOption[] = [
    { value: "local", label: "Local" },
    { value: "remote", label: "R2" },
];

const log = (e: Event) =>
    // eslint-disable-next-line no-console
    console.log("change →", (e as CustomEvent).detail);

// 0–5 tier range, value 2 selected.
export const Tiers = () =>
    html`<rune-segmented .options=${tiers} value="2" label="Keep last" @change=${log}></rune-segmented>`;

// Binary scope switch.
export const Scope = () =>
    html`<rune-segmented .options=${scope} value="local" label="Scope" @change=${log}></rune-segmented>`;

// Disabled.
export const Disabled = () =>
    html`<rune-segmented disabled .options=${tiers} value="2" label="Keep last"></rune-segmented>`;
