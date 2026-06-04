import { html } from "lit";
import "./retention-rules";
import type { RetentionRules } from "./retention-model";

export default {
    title: "Components / Retention Rules",
    component: "retention-rules",
    parameters: {
        docs: {
            description: {
                component:
                    "Editable Borg-style tier picker (design-log/033, wired by /039, redesigned). Each " +
                    "tier is one collapsed row: label + illustrative dated dot-preview + uncapped " +
                    "`rune-stepper`. A Local·R2 scope switch edits both sides. Explains the policy — " +
                    "never a dry-run over real backups. Emits `change` {local, remote}.",
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

export const Default = () =>
    pane(html`<retention-rules .now=${NOW} .local=${r({ keepLast: 2 })} @change=${log}></retention-rules>`);

export const Paranoid = () =>
    pane(
        html`<retention-rules
            .now=${NOW}
            .local=${r({ keepLast: 5, keepDaily: 5, keepWeekly: 4, keepMonthly: 3 })}
            @change=${log}
        ></retention-rules>`,
    );

export const Minimalist = () =>
    pane(html`<retention-rules .now=${NOW} .local=${r({ keepLast: 1 })} @change=${log}></retention-rules>`);

// Uncapped count → 8 dated dots + a "+N" overflow.
export const Uncapped = () =>
    pane(html`<retention-rules .now=${NOW} .local=${r({ keepDaily: 14 })} @change=${log}></retention-rules>`);

// keep_last:0 → caution copy.
export const KeepLastZero = () =>
    pane(html`<retention-rules .now=${NOW} .local=${r({ keepWeekly: 3 })} @change=${log}></retention-rules>`);
