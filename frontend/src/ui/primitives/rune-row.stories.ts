import { html } from "lit";
import "./rune-row";

export default {
    title: "Primitives / Rune Row",
    component: "rune-row",
    parameters: {
        docs: {
            description: {
                component:
                    "List row with leading / default / trailing slots. When pressable, emits `press`. " +
                    "External layout overrides via `::part(container)` and `--rune-row-template` for grid columns. " +
                    "HIG: https://developer.apple.com/design/human-interface-guidelines/lists-and-tables",
            },
        },
    },
};

const onPress = (e: Event) => {
    // eslint-disable-next-line no-console
    console.log("row press", (e as CustomEvent).detail);
};

export const Static = () => html`
    <div style="padding:var(--space-4); max-width:380px; display:flex; flex-direction:column; gap:var(--space-1);">
        <rune-row>
            <span slot="leading">●</span>
            <span>Static row</span>
            <span slot="trailing" style="color:var(--text-faint);">meta</span>
        </rune-row>
        <rune-row>
            <span slot="leading">●</span>
            <span>Another</span>
            <span slot="trailing" style="color:var(--text-faint);">meta</span>
        </rune-row>
    </div>
`;

export const Pressable = () => html`
    <div style="padding:var(--space-4); max-width:380px; display:flex; flex-direction:column; gap:var(--space-1);">
        <rune-row pressable aria-label="Open item one" @press=${onPress}>
            <span slot="leading">▸</span>
            <span>Pressable — click or Enter / Space</span>
        </rune-row>
        <rune-row pressable active aria-label="Selected item" @press=${onPress}>
            <span slot="leading">▸</span>
            <span>Active (selected)</span>
        </rune-row>
        <rune-row pressable disabled aria-label="Disabled" @press=${onPress}>
            <span slot="leading">▸</span>
            <span>Disabled</span>
        </rune-row>
    </div>
`;

export const CustomLayout = () => html`
    <div style="padding:var(--space-4); max-width:380px;">
        <rune-row
            pressable
            style="--rune-row-template: 60px 1fr 14px;"
            aria-label="Address row preview"
            @press=${onPress}
        >
            <span slot="leading" style="color:var(--text-muted);">LAN</span>
            <span style="font-family:var(--font-mono); color:var(--text-strong);">192.168.1.42:25565</span>
            <span slot="trailing" style="color:var(--text-faint);">⧉</span>
        </rune-row>
    </div>
`;
