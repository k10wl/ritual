import { html } from "lit";
import "./rune-button";

export default {
    title: "Primitives / Rune Button",
    component: "rune-button",
    parameters: {
        docs: {
            description: {
                component:
                    "Filled action element. Variants `primary | tinted | plain`, sizes `sm | md | lg`. " +
                    "Emits `press` (not `click`) with origin detail. " +
                    "HIG: https://developer.apple.com/design/human-interface-guidelines/buttons",
            },
        },
    },
};

const onPress = (e: Event) => {
    const detail = (e as CustomEvent).detail;
    // eslint-disable-next-line no-console
    console.log("press", detail);
};

export const Variants = () => html`
    <div style="display:flex; gap:var(--space-3); flex-wrap:wrap; padding:var(--space-4);">
        <rune-button variant="primary" @press=${onPress}>Start</rune-button>
        <rune-button variant="tinted" @press=${onPress}>Cancel</rune-button>
        <rune-button variant="plain"  @press=${onPress}>Skip</rune-button>
    </div>
`;

export const Sizes = () => html`
    <div style="display:flex; gap:var(--space-3); align-items:center; padding:var(--space-4);">
        <rune-button variant="primary" size="sm" @press=${onPress}>Small</rune-button>
        <rune-button variant="primary" size="md" @press=${onPress}>Medium</rune-button>
        <rune-button variant="primary" size="lg" @press=${onPress}>Large</rune-button>
    </div>
`;

export const States = () => html`
    <div style="display:flex; gap:var(--space-3); padding:var(--space-4);">
        <rune-button variant="primary">Idle</rune-button>
        <rune-button variant="primary" disabled>Disabled</rune-button>
        <rune-button variant="primary" loading>Loading</rune-button>
    </div>
`;

export const WithSlots = () => html`
    <div style="display:flex; gap:var(--space-3); padding:var(--space-4);">
        <rune-button variant="tinted">
            <span slot="leading">▸</span>
            Play
        </rune-button>
        <rune-button variant="tinted">
            Done
            <span slot="trailing">✓</span>
        </rune-button>
    </div>
`;
