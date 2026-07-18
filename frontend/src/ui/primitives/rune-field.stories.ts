import { html } from "lit";
import "./rune-field";
import { composeValidators, type RuneFieldValidator } from "./rune-field";

const required: RuneFieldValidator = (v) =>
    v.trim() === "" ? "Required." : null;

const numeric: RuneFieldValidator = (v) =>
    v === "" || !Number.isNaN(Number(v)) ? null : "Must be a number.";

const range = (lo: number, hi: number): RuneFieldValidator => (v) => {
    if (v === "") return null;
    const n = Number(v);
    return n < lo || n > hi ? `Must be between ${lo} and ${hi}.` : null;
};

const divisibleBy = (d: number): RuneFieldValidator => (v) => {
    if (v === "") return null;
    return Number(v) % d === 0 ? null : `Must be a multiple of ${d}.`;
};

export default {
    title: "Primitives / Rune Field",
    component: "rune-field",
    parameters: {
        docs: {
            description: {
                component:
                    "Labelled input — HIG label-above text-field. Form-associated custom element. " +
                    "Attributes: type (text|number), label, hint, value, placeholder, disabled, invalid. " +
                    "Property: validate (RuneFieldValidator). Compose multiple rules with composeValidators(...). " +
                    "type=\"number\" only switches the mobile inputmode; constraints live in the validator. " +
                    "HIG: https://developer.apple.com/design/human-interface-guidelines/text-fields",
            },
        },
    },
};

export const Text = () => html`
    <div style="padding:var(--space-4); max-width:380px;">
        <rune-field
            label="Display name"
            placeholder="e.g. ritual.local"
            hint="Shown to peers in the lobby."
        ></rune-field>
    </div>
`;

export const Numeric = () => html`
    <div style="padding:var(--space-4); max-width:380px; display:flex; flex-direction:column; gap:var(--space-4);">
        <rune-field
            type="number"
            label="Port"
            value="25565"
            hint="Range 1024–65535."
            .validate=${composeValidators(numeric, range(1024, 65535))}
        ></rune-field>
        <rune-field
            type="number"
            label="Memory (MB)"
            value="2048"
            hint="At least 1024 MB recommended."
            .validate=${composeValidators(numeric, range(512, 16384))}
        ></rune-field>
    </div>
`;

export const ComposedRules = () => html`
    <div style="padding:var(--space-4); max-width:380px;">
        <rune-field
            type="number"
            label="Buffer size"
            placeholder="multiple of 15, 15–150"
            hint="FizzBuzz-shaped: required, numeric, in range, divisible by 15."
            .validate=${composeValidators(
                required,
                numeric,
                range(15, 150),
                divisibleBy(15),
            )}
        ></rune-field>
    </div>
`;

export const States = () => html`
    <div style="padding:var(--space-4); max-width:380px; display:flex; flex-direction:column; gap:var(--space-4);">
        <rune-field label="Idle" placeholder="type here"></rune-field>
        <rune-field label="Pre-filled" value="ritual"></rune-field>
        <rune-field label="Invalid" value="80" invalid hint="Must be ≥ 1024."></rune-field>
        <rune-field label="Disabled" value="locked" disabled></rune-field>
    </div>
`;
