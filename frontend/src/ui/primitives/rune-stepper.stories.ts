import { html } from "lit";
import "./rune-stepper";

export default {
    title: "Primitives / Rune Stepper",
    component: "rune-stepper",
    parameters: {
        docs: {
            description: {
                component:
                    "Compact `− value +` stepper for a small bounded integer (design-log/033 §Q1 " +
                    "redesign). role=spinbutton, ↑/→/↓/←/Home/End. Emits `change` {value}. Drives the " +
                    "retention tier counts (0–5).",
            },
        },
    },
};

const log = (e: Event) =>
    // eslint-disable-next-line no-console
    console.log("change →", (e as CustomEvent).detail);

export const Default = () => html`<rune-stepper value="2" label="Keep last" @change=${log}></rune-stepper>`;

export const AtMin = () => html`<rune-stepper value="0" label="Keep daily" @change=${log}></rune-stepper>`;

export const AtMax = () => html`<rune-stepper value="5" label="Keep weekly" @change=${log}></rune-stepper>`;

export const Disabled = () => html`<rune-stepper disabled value="2" label="Keep last"></rune-stepper>`;
