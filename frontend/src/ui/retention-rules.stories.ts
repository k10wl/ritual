import { html } from "lit";
import "./retention-rules";
import { sample, type RetentionRules } from "./retention-model";

export default {
    title: "Components / Retention Rules",
    component: "retention-rules",
    parameters: {
        docs: {
            description: {
                component:
                    "Editable Borg-style tier picker (design-log/033) wired end-to-end by /039. " +
                    "A Local·R2 scope switch over four 0–5 `rune-segmented` tiers, with a live " +
                    "kept-vs-pruned summary, legend, and timeline computed by the real `mark()` union. " +
                    "Emits `change` {local, remote}.",
            },
        },
    },
};

const NOW = new Date("2026-06-04T12:00:00Z");

const pane = (inner: unknown) => html`
    <div style="width:420px; background:var(--stone-deep); border:1px solid var(--stone-bevel);
                border-radius:var(--radius-lg); padding:var(--space-4);">
        ${inner}
    </div>
`;

const log = (e: Event) =>
    // eslint-disable-next-line no-console
    console.log("change →", (e as CustomEvent).detail);

const r = (o: Partial<RetentionRules>): RetentionRules => ({
    keepLast: 0,
    keepDaily: 0,
    keepWeekly: 0,
    keepMonthly: 0,
    ...o,
});

// Default policy (keep_last:2).
export const Default = () =>
    pane(html`<retention-rules .now=${NOW} .local=${r({ keepLast: 2 })} @change=${log}></retention-rules>`);

// Paranoid — every tier high.
export const Paranoid = () =>
    pane(
        html`<retention-rules
            .now=${NOW}
            .local=${r({ keepLast: 5, keepDaily: 5, keepWeekly: 4, keepMonthly: 3 })}
            @change=${log}
        ></retention-rules>`,
    );

// Minimalist — keep just the latest.
export const Minimalist = () =>
    pane(html`<retention-rules .now=${NOW} .local=${r({ keepLast: 1 })} @change=${log}></retention-rules>`);

// keep_last:0 → caution copy.
export const KeepLastZero = () =>
    pane(html`<retention-rules .now=${NOW} .local=${r({ keepDaily: 3 })} @change=${log}></retention-rules>`);

// Real (well, synthetic-but-explicit) backups passed in.
export const CustomHistory = () =>
    pane(
        html`<retention-rules
            .now=${NOW}
            .local=${r({ keepLast: 2, keepWeekly: 2, keepMonthly: 2 })}
            .localBackups=${sample(NOW)}
            @change=${log}
        ></retention-rules>`,
    );
