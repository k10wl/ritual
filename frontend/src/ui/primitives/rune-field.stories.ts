import { html } from "lit";
import "./rune-field";

export default {
    title: "Primitives / Rune Field",
    component: "rune-field",
    parameters: {
        docs: {
            description: {
                component:
                    "Labelled input — HIG label-above text-field. Form-associated custom element. " +
                    "Attributes: type (text|number), label, hint, value, min, max, step, placeholder, disabled, invalid. " +
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

export const Number = () => html`
    <div style="padding:var(--space-4); max-width:380px; display:flex; flex-direction:column; gap:var(--space-4);">
        <rune-field
            type="number"
            label="Port"
            value="25565"
            min="1024"
            max="65535"
            step="1"
            hint="Range 1024–65535."
        ></rune-field>
        <rune-field
            type="number"
            label="Memory (MB)"
            value="2048"
            min="512"
            max="16384"
            step="256"
            hint="At least 1024 MB recommended."
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
