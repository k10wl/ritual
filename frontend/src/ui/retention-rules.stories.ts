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
                    "Editable Borg-style tier picker (design-log/033, wired by /039, re-revised to " +
                    "ELI5). One row per keep-type: a `Keep …` label, an uncapped `rune-stepper`, and a " +
                    "plain-English sentence that rewrites itself as the number changes. No timeline or " +
                    "dots — the meaning lives in words. A Local·Remote scope switch edits both sides. " +
                    "Emits `change` {local, remote}.",
            },
        },
    },
};

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

export const Default = () => pane(html`<retention-rules .local=${r({ keepLast: 2 })} @change=${log}></retention-rules>`);

export const Paranoid = () =>
    pane(
        html`<retention-rules
            .local=${r({ keepLast: 5, keepDaily: 5, keepWeekly: 4, keepMonthly: 3 })}
            @change=${log}
        ></retention-rules>`,
    );

export const Minimalist = () =>
    pane(html`<retention-rules .local=${r({ keepLast: 1 })} @change=${log}></retention-rules>`);

// High counts stay legible — words, not clumped dots.
export const HighCounts = () =>
    pane(html`<retention-rules .local=${r({ keepLast: 9, keepWeekly: 11 })} @change=${log}></retention-rules>`);

// keep_last:0 → caution copy.
export const KeepLastZero = () =>
    pane(html`<retention-rules .local=${r({ keepWeekly: 3 })} @change=${log}></retention-rules>`);
