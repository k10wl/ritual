import { html } from "lit";
import "./rune-progress";

export default {
    title: "Primitives / Rune Progress",
    component: "rune-progress",
    parameters: {
        docs: {
            description: {
                component:
                    "Progress indicator (ring or linear). Value 0–1; omit for indeterminate. " +
                    "Override color via `--rune-progress-color`. " +
                    "HIG: https://developer.apple.com/design/human-interface-guidelines/progress-indicators",
            },
        },
    },
};

export const Ring = () => html`
    <div style="display:flex; gap:var(--space-4); align-items:center; padding:var(--space-4);">
        <rune-progress variant="ring" value="0.1" style="--rune-progress-size: 32px;"></rune-progress>
        <rune-progress variant="ring" value="0.45" style="--rune-progress-size: 32px;"></rune-progress>
        <rune-progress variant="ring" value="0.8" style="--rune-progress-size: 32px;"></rune-progress>
        <rune-progress variant="ring" value="1" style="--rune-progress-size: 32px;"></rune-progress>
    </div>
`;

export const Linear = () => html`
    <div style="padding:var(--space-4); max-width:320px; display:flex; flex-direction:column; gap:var(--space-3);">
        <rune-progress variant="linear" value="0.15"></rune-progress>
        <rune-progress variant="linear" value="0.55"></rune-progress>
        <rune-progress variant="linear" value="0.92"></rune-progress>
    </div>
`;

export const Indeterminate = () => html`
    <div style="display:flex; gap:var(--space-4); align-items:center; padding:var(--space-4);">
        <rune-progress variant="ring" style="--rune-progress-size: 32px;"></rune-progress>
        <rune-progress variant="linear" style="width: 240px;"></rune-progress>
    </div>
`;

export const StateColors = () => html`
    <div style="display:flex; gap:var(--space-4); align-items:center; padding:var(--space-4);">
        <rune-progress variant="ring" value="0.6"
                       style="--rune-progress-size: 40px; --rune-progress-color: var(--state-idle);"></rune-progress>
        <rune-progress variant="ring" value="0.6"
                       style="--rune-progress-size: 40px; --rune-progress-color: var(--state-prep);"></rune-progress>
        <rune-progress variant="ring" value="0.6"
                       style="--rune-progress-size: 40px; --rune-progress-color: var(--state-run);"></rune-progress>
        <rune-progress variant="ring" value="0.6"
                       style="--rune-progress-size: 40px; --rune-progress-color: var(--state-final);"></rune-progress>
        <rune-progress variant="ring" value="0.6"
                       style="--rune-progress-size: 40px; --rune-progress-color: var(--state-fail);"></rune-progress>
    </div>
`;
