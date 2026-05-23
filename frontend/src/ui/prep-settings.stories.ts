import { html } from "lit";
import "./prep-settings";
import "./primitives/rune-button";
import type { PrepSettingsEl, PrepSettingsChangeDetail } from "./prep-settings";

export default {
    title: "Components / Prep Settings",
    component: "prep-settings",
    parameters: {
        docs: {
            description: {
                component:
                    "Idle-only advanced-settings disclosure. Uses <rune-disclosure> + <rune-field> × 2. " +
                    "See design-log/014-prep-advanced-settings.md.",
            },
        },
    },
};

const onChange = (e: Event) => {
    const detail = (e as CustomEvent<PrepSettingsChangeDetail>).detail;
    // eslint-disable-next-line no-console
    console.log("prep-settings change", detail);
};

const onSubmit = (e: Event) => {
    const detail = (e as CustomEvent).detail;
    // eslint-disable-next-line no-console
    console.log("prep-settings submit", detail);
};

export const Closed = () => html`
    <div style="padding:var(--space-4); max-width:380px;">
        <prep-settings @change=${onChange} @submit=${onSubmit}></prep-settings>
    </div>
`;

export const WithCustomDefaults = () => html`
    <div style="padding:var(--space-4); max-width:380px;">
        <prep-settings
            .config=${{ port: 30000, memoryMB: 8192 }}
            @change=${onChange}
            @submit=${onSubmit}
        ></prep-settings>
    </div>
`;

export const IdleSurface = () => {
    const onStart = (e: Event) => {
        const root = (e.currentTarget as HTMLElement).parentElement!;
        const settings = root.querySelector("prep-settings") as PrepSettingsEl | null;
        const values = settings?.read();
        // eslint-disable-next-line no-console
        console.log("Start pressed; values:", values);
    };
    return html`
        <div style="padding:var(--space-5); max-width:420px; display:flex; flex-direction:column; gap:var(--space-4); align-items:stretch;">
            <prep-settings @change=${onChange}></prep-settings>
            <rune-button variant="primary" size="lg" @press=${onStart}>Start</rune-button>
        </div>
    `;
};
