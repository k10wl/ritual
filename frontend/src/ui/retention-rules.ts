/**
 * Retention rules — editable Borg-style tier picker, explained at an ELI5 level
 * (design-log/033; wired by /039; re-revised 2026-06-04 to plain language;
 * staged-edits rework 2026-06-05 per design-log/045 §D).
 *
 * One row per keep-type: a friendly `Keep …` label, an uncapped `rune-stepper`,
 * and a single plain-English sentence that rewrites itself as the number
 * changes (e.g. "Always keep your 9 newest backups."). A Local·Remote
 * `rune-segmented` edits both sides; a keep_last:0 caution remains.
 *
 * **Staged-edit model (design-log/045 §D / §Q6 decided):** stepper taps mutate
 * a local *draft* state; the saved baseline (the `local` / `remote` props the
 * host passes in) is unchanged until Apply. When the draft differs from the
 * baseline, an **Apply bar** appears with Apply (primary) + Discard (tinted).
 * Apply emits `apply` { local, remote }; the host persists via
 * `setRetentionRules` AND runs `applyRetentionNow` to prune immediately. The
 * old auto-save-on-every-tap behaviour is gone — the user now has a clear
 * commit moment and can twiddle without committing.
 *
 * Presentational + pure: no wall-clock read; no Wails-api calls; renders from
 * either draft (when dirty) or baseline (clean).
 */

import { LitElement, css, html, nothing } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import "./primitives/rune-segmented";
import "./primitives/rune-stepper";
import "./primitives/rune-button";
import "./primitives/decoder";
import type { RetentionRules, Tier } from "./retention-model";

export type RetentionScope = "local" | "remote";

/** Emitted on every staged edit (`change`) and on Apply (`apply`). Same shape;
 * the host listens to `apply` to persist + prune, and may ignore `change`
 * (kept for testability + future hooks). */
export interface RetentionChangeDetail {
    local: RetentionRules;
    remote: RetentionRules;
}

const equalRules = (a: RetentionRules, b: RetentionRules) =>
    a.keepLast === b.keepLast &&
    a.keepDaily === b.keepDaily &&
    a.keepWeekly === b.keepWeekly &&
    a.keepMonthly === b.keepMonthly;

// One active tier in the nested cascade: tier, subtle label, selected count, and
// how far back it reaches (days, the shared scale that drives the nesting widths).
interface CascadeTier {
    tier: Tier;
    label: string;
    n: number;
    reach: number;
}

const TIERS: { key: keyof RetentionRules; label: string; tier: Tier; unit: string }[] = [
    { key: "keepLast", label: "Keep last", tier: "last", unit: "" },
    { key: "keepDaily", label: "Keep daily", tier: "daily", unit: "day" },
    { key: "keepWeekly", label: "Keep weekly", tier: "weekly", unit: "week" },
    { key: "keepMonthly", label: "Keep monthly", tier: "monthly", unit: "month" },
];

// R2 is just one provider; users think in terms of where the backup lives, not the
// vendor — so the label is the generic "Remote".
const SCOPE_OPTS = [
    { value: "local", label: "Local" },
    { value: "remote", label: "Remote" },
];

// Temperature ramp for the cascade band: recent = warm green, older = colder and
// darker (teal → blue → indigo). Keyed by tier so "month" is always the coldest.
const TIER_RAMP: Record<Tier, string> = {
    last: "hsl(150 46% 44%)",
    daily: "hsl(192 44% 40%)",
    weekly: "hsl(218 46% 38%)",
    monthly: "hsl(244 40% 34%)",
};

const DEFAULT_RULES: RetentionRules = { keepLast: 2, keepDaily: 0, keepWeekly: 0, keepMonthly: 0 };

const plural = (n: number, unit: string) => `${n} ${unit}${n === 1 ? "" : "s"}`;

// Coerce a possibly-missing tier (the backend omits zero fields via omitempty, so
// they arrive undefined) to a real number — no undefined/NaN reaches the view.
const num = (v: unknown): number => Number(v) || 0;
const normalizeRules = (r: Partial<RetentionRules> | undefined | null): RetentionRules => ({
    keepLast: num(r?.keepLast),
    keepDaily: num(r?.keepDaily),
    keepWeekly: num(r?.keepWeekly),
    keepMonthly: num(r?.keepMonthly),
});

// tierPhrase → a cascade step in the handoff narrative ("your 2 newest", "1 a day
// for 8 days"). The window length ("for 8 days") is exactly where this rule hands
// off to the next, coarser one.
function tierPhrase(tier: Tier, n: number): string {
    switch (tier) {
        case "last":
            return n === 1 ? "your newest backup" : `your ${n} newest`;
        case "daily":
            return `1 a day for ${plural(n, "day")}`;
        case "weekly":
            return `1 a week for ${plural(n, "week")}`;
        case "monthly":
            return `1 a month for ${plural(n, "month")}`;
    }
}

// keptTotal → estimated distinct backups kept. Tiers OVERLAP: a coarser rule's
// representative (newest-of-week/month) is usually a backup a finer rule already
// kept, so it adds new backups only beyond the finer rule's coverage. Assumes ~1
// backup/day (keep_last ≈ days). It's why the total is below the sum of the rules.
export function keptTotal(rules: RetentionRules): number {
    const covLast = rules.keepLast; // days covered by keep_last (~1/day)
    const dailyNew = Math.max(0, rules.keepDaily - covLast);
    const covDaily = Math.max(covLast, rules.keepDaily);
    const weeklyNew = Math.max(0, rules.keepWeekly - Math.ceil(covDaily / 7));
    const covWeekly = Math.max(covDaily, rules.keepWeekly * 7);
    const monthlyNew = Math.max(0, rules.keepMonthly - Math.ceil(covWeekly / 30));
    return rules.keepLast + dailyNew + weeklyNew + monthlyNew;
}

// reachPhrase → how far back the coarsest contributing tier extends. keep_last is a
// count, not a duration, so it has no reach.
function reachPhrase(tier: Tier, n: number): string {
    switch (tier) {
        case "last":
            return "";
        case "daily":
            return plural(n, "day");
        case "weekly":
            return plural(n, "week");
        case "monthly":
            return plural(n, "month");
    }
}

// explain → the one plain sentence shown under each rule. ELI5: no jargon, says
// exactly what the number does, kept short enough to stay on one line. n === 0
// reads as off.
export function explain(tier: Tier, n: number): string {
    if (n === 0) return "Off — none kept this way.";
    switch (tier) {
        case "last":
            return n === 1 ? "Keep your newest backup." : `Keep your ${n} newest backups.`;
        case "daily":
            return n === 1 ? "Keep today's backup." : `Keep one a day for ${n} days.`;
        case "weekly":
            return n === 1 ? "Keep this week's backup." : `Keep one a week for ${n} weeks.`;
        case "monthly":
            return n === 1 ? "Keep this month's backup." : `Keep one a month for ${n} months.`;
    }
}

// describePolicy → one sentence for the whole policy; used as the a11y label so
// screen readers get the full meaning in one read (design-log/033 §Redesign).
export function describePolicy(r: RetentionRules): string {
    const parts: string[] = [];
    if (r.keepLast === 1) parts.push("the most recent backup");
    else if (r.keepLast > 1) parts.push(`the ${r.keepLast} most recent backups`);
    for (const t of TIERS) {
        if (t.key === "keepLast") continue;
        const n = r[t.key];
        if (n > 0) parts.push(`1 per ${t.unit} for ${plural(n, t.unit)}`);
    }
    if (parts.length === 0) return "Nothing is kept — every backup is eventually removed.";
    const joined =
        parts.length === 1 ? parts[0] : `${parts.slice(0, -1).join(", ")}, and ${parts[parts.length - 1]}`;
    return `Keeps ${joined}.`;
}

@customElement("retention-rules")
export class RetentionRulesEl extends LitElement {
    // Baseline — the persisted rules the host hands down. Treated as the
    // single source of truth for "what's saved right now"; the draft is what
    // the user has typed but not yet committed (design-log/045 §D).
    @property({ attribute: false }) local: RetentionRules = { ...DEFAULT_RULES };
    @property({ attribute: false }) remote: RetentionRules = { ...DEFAULT_RULES };

    @state() private _scope: RetentionScope = "local";

    // Drafts — null means "no staged edits, render from baseline". On the first
    // stepper tap they fork from baseline; Apply / Discard / new baseline (host
    // re-load) clears them back to null.
    @state() private _draftLocal: RetentionRules | null = null;
    @state() private _draftRemote: RetentionRules | null = null;

    static styles = css`
        :host {
            display: block;
            font-family: var(--font-mono);
            color: var(--text);
        }
        .switch {
            margin-bottom: var(--space-4);
        }
        .rules {
            display: grid;
            gap: var(--space-4);
        }
        .rule {
            display: grid;
            gap: var(--space-1);
        }
        /* label left, stepper pinned right — the steppers align in a column. */
        .head {
            display: grid;
            grid-template-columns: 1fr auto;
            align-items: center;
            gap: var(--space-2);
        }
        .rule-label {
            color: var(--text-strong);
            white-space: nowrap;
        }
        .explain {
            color: var(--text-muted);
            font-size: var(--fs-caption);
            line-height: 1.4;
            white-space: nowrap;
        }
        .rule[data-zero] .rule-label,
        .rule[data-zero] .explain {
            color: var(--text-faint);
        }

        /* Cascade: the combined outcome. Every tier reaches back from the NEWEST
           backup, so the bars are nested — all anchored at the left edge (= the
           newest backup, where every tier overlaps), each as long as its reach. */
        .cascade {
            margin-top: var(--space-4);
            display: grid;
            gap: var(--space-1);
        }
        .cascade-ends {
            display: flex;
            justify-content: space-between;
            font-size: var(--fs-caption);
            color: var(--text-faint);
        }
        .nest {
            display: grid;
            gap: 4px;
        }
        .nest-row {
            display: grid;
            grid-template-columns: 3.25rem 1fr;
            align-items: center;
            gap: var(--space-2);
        }
        /* Subtle word labels (last / day / week / month) — quiet, not shouting. */
        .nest-label {
            font-size: var(--fs-caption);
            color: var(--text-faint);
            white-space: nowrap;
        }
        .bar-track {
            position: relative;
            height: 20px;
        }
        .bar {
            position: absolute;
            inset-block: 0;
            left: 0;
            min-width: 1.9rem;
            display: flex;
            align-items: center;
            justify-content: flex-end;
            padding: 0 6px;
            border-radius: var(--radius-sm);
        }
        .bar-count {
            font-size: var(--fs-caption);
            color: #fff;
            font-variant-numeric: tabular-nums;
        }
        .summary {
            margin-top: var(--space-2);
            color: var(--text-muted);
            font-size: var(--fs-caption);
            line-height: 1.4;
        }
        .caution {
            margin-top: var(--space-4);
            padding: var(--space-3);
            border-radius: var(--radius-sm);
            color: var(--warning, #e0a106);
            background: color-mix(in srgb, var(--warning, #e0a106) 12%, transparent);
            border: 1px solid color-mix(in srgb, var(--warning, #e0a106) 40%, transparent);
            line-height: 1.5;
            font-size: var(--fs-caption);
        }

        /* Apply bar — surfaces only when there are staged edits
           (design-log/045 §D). Quiet hint on the left, Discard + Apply pinned
           right. Same register as the rest of Advanced: monospace, low-key,
           the primary action carries the weight. */
        .apply-bar {
            margin-top: var(--space-4);
            padding: var(--space-3);
            border-top: 1px solid var(--stone-bevel);
            display: grid;
            grid-template-columns: 1fr auto;
            align-items: center;
            gap: var(--space-3);
        }
        .apply-hint {
            color: var(--text-muted);
            font-size: var(--fs-caption);
        }
        .apply-actions {
            display: flex;
            gap: var(--space-2);
        }
    `;

    // Effective rules for the active scope: draft when dirty, baseline when
    // clean. Always normalised (zero-default-fill) — never `undefined`-leaning
    // since the prune engine treats {0,0,0,0} as defaults.
    private get rules(): RetentionRules {
        if (this._scope === "remote") {
            return normalizeRules(this._draftRemote ?? this.remote);
        }
        return normalizeRules(this._draftLocal ?? this.local);
    }

    /** Combined effective draft (or baseline if clean) for both sides — what
     * Apply emits. */
    private effectiveBoth(): { local: RetentionRules; remote: RetentionRules } {
        return {
            local: normalizeRules(this._draftLocal ?? this.local),
            remote: normalizeRules(this._draftRemote ?? this.remote),
        };
    }

    /** True iff the user has touched either side since the last Apply/Discard. */
    private isDirty(): boolean {
        const eff = this.effectiveBoth();
        return !equalRules(eff.local, normalizeRules(this.local)) ||
            !equalRules(eff.remote, normalizeRules(this.remote));
    }

    // Reset drafts to null whenever the host hands down a new baseline (e.g.
    // after Apply success the host re-loads and pipes fresh `local` / `remote`
    // values back in). Without this the staged edits would shadow the saved
    // state forever after a successful Apply.
    willUpdate(changed: Map<string, unknown>) {
        if (changed.has("local") && this._draftLocal && equalRules(this._draftLocal, normalizeRules(this.local))) {
            this._draftLocal = null;
        }
        if (changed.has("remote") && this._draftRemote && equalRules(this._draftRemote, normalizeRules(this.remote))) {
            this._draftRemote = null;
        }
    }

    // Shared decoder tuning: this panel shows many texts at once, so idle jitter is
    // rare (every ~14–36s, not 2–5s) and the decode-on-change is fast (snappy ticks,
    // few rounds). Keeps the effect without a wall of constant shimmer.
    #decoder(text: string, cls: string) {
        return html`<rune-decoder
            class=${cls}
            .text=${text}
            idle-min-ms="14000"
            idle-max-ms="36000"
            idle-tick-ms="55"
            splash-tick-ms="40"
            .splashRounds=${[3, 6] as const}
        ></rune-decoder>`;
    }

    render() {
        const rules = this.rules;
        const dirty = this.isDirty();
        return html`
            <div class="switch">
                <rune-segmented
                    .options=${SCOPE_OPTS}
                    value=${this._scope}
                    label="Scope"
                    @change=${this.#onScope}
                ></rune-segmented>
            </div>
            <div class="rules" role="group" aria-label=${describePolicy(rules)}>
                ${TIERS.map((t) => this.#renderRule(t))}
            </div>
            ${this.#renderCascade(rules)}
            ${rules.keepLast === 0 ? this.#renderCaution(rules) : null}
            ${dirty ? this.#renderApplyBar() : nothing}
        `;
    }

    // Apply bar (design-log/045 §D) — appears only when the draft differs from
    // the saved baseline. Two actions: Apply (primary, persists + prunes via
    // the host) and Discard (tinted, drops the staged edits and restores the
    // baseline rules in-place).
    #renderApplyBar() {
        return html`
            <div class="apply-bar" role="region" aria-label="Pending retention edits">
                ${this.#decoder("Unsaved changes.", "apply-hint")}
                <div class="apply-actions">
                    <rune-button variant="tinted" @press=${this.#discard}>Discard</rune-button>
                    <rune-button variant="primary" @press=${this.#apply}>Apply</rune-button>
                </div>
            </div>
        `;
    }

    #renderRule(t: { key: keyof RetentionRules; label: string; tier: Tier }) {
        const count = this.rules[t.key];
        return html`<div class="rule" data-tier=${t.label} ?data-zero=${count === 0}>
            <div class="head">
                ${this.#decoder(t.label, "rule-label")}
                <rune-stepper
                    .value=${count}
                    min="0"
                    .max=${Infinity}
                    label=${t.label}
                    @change=${(e: CustomEvent<{ value: number }>) => {
                        // Contain the primitive's composed `change` (see #onScope)
                        // so only our own {local, remote} `change` reaches the host.
                        e.stopPropagation();
                        this.#onTier(t.key, e.detail.value);
                    }}
                ></rune-stepper>
            </div>
            ${this.#decoder(explain(t.tier, count), "explain")}
        </div>`;
    }

    // Active tiers, finest→coarsest, with how far each reaches back (in days, the
    // shared scale for nesting; last/daily ≈ daily cadence, weekly ×7, monthly ×30).
    // Every tier is anchored at the newest backup, so coarser reaches CONTAIN finer
    // ones — the nesting makes the overlap (they coincide at the newest) visible.
    #activeTiers(rules: RetentionRules): CascadeTier[] {
        return (
            [
                { tier: "last", label: "last", n: rules.keepLast, reach: rules.keepLast },
                { tier: "daily", label: "day", n: rules.keepDaily, reach: rules.keepDaily },
                { tier: "weekly", label: "week", n: rules.keepWeekly, reach: rules.keepWeekly * 7 },
                { tier: "monthly", label: "month", n: rules.keepMonthly, reach: rules.keepMonthly * 30 },
            ] as CascadeTier[]
        ).filter((t) => t.n > 0);
    }

    #renderCascade(rules: RetentionRules) {
        const tiers = this.#activeTiers(rules);
        if (!tiers.length) return null;
        const maxReach = Math.max(...tiers.map((t) => t.reach), 1);

        // Narrative: total kept + the handoff (each rule reaches further back than
        // the finer one), plus an overlap note when the total is below the naive sum.
        const phrases = tiers.map((t) => tierPhrase(t.tier, t.n));
        const coarsest = tiers[tiers.length - 1];
        const reach = reachPhrase(coarsest.tier, coarsest.n);
        const total = keptTotal(rules);
        const shownSum = tiers.reduce((acc, t) => acc + t.n, 0);
        const overlap = total < shownSum ? " Rules overlap, so it's fewer than adding them up." : "";
        const head = `Keeps about ${total} backup${total === 1 ? "" : "s"}`;
        const body =
            tiers.length === 1
                ? phrases[0]
                : `${phrases.join(", then ")}${reach ? `, reaching ~${reach} back` : ""}`;
        const summary = `${head}: ${body}.${overlap}`;

        return html`
            <div class="cascade" aria-hidden="true">
                <div class="cascade-ends">${this.#decoder("Newer", "")}${this.#decoder("Older →", "")}</div>
                <div class="nest">
                    ${tiers.map(
                        (t) => html`<div class="nest-row" data-tier=${t.tier}>
                            ${this.#decoder(t.label, "nest-label")}
                            <div class="bar-track">
                                <div
                                    class="bar"
                                    style=${`width:${(t.reach / maxReach) * 100}%;background:${TIER_RAMP[t.tier]}`}
                                >
                                    ${this.#decoder(`${t.n}`, "bar-count")}
                                </div>
                            </div>
                        </div>`,
                    )}
                </div>
                ${this.#decoder(summary, "summary")}
            </div>
        `;
    }

    // Truthful keep_last:0 note. With keep_last off the single newest backup still
    // survives via any daily/weekly/monthly rule (it's the newest of its day/week/
    // month); it only disappears when EVERY rule is 0. What keep_last uniquely adds
    // is keeping more than one recent backup (e.g. several taken the same day).
    #renderCaution(rules: RetentionRules) {
        const others = rules.keepDaily + rules.keepWeekly + rules.keepMonthly;
        const text =
            others === 0
                ? "Every rule is set to 0 — no backups will be kept."
                : "With “keep last” off, only one backup per day, week, or month is kept — extra backups taken in the same period won't all survive.";
        return html`<p class="caution">${this.#decoder(text, "")}</p>`;
    }

    // Contain the inner <rune-segmented> `change` (design-log/045 post-ship):
    // the primitive fires `change` with `composed: true`, so without this stop
    // it bubbles out of our shadow root and is mistaken by the host for *our*
    // semantic `change` event — except its detail is `{value}`, not
    // `{local, remote}`, so the host reads `undefined` rules and the picker
    // collapses to all-zeros on a scope flip. Our public `change` is emitted
    // explicitly by #onTier / #discard; raw primitive events stay inside.
    #onScope = (e: CustomEvent<{ value: string }>) => {
        e.stopPropagation();
        this._scope = e.detail.value as RetentionScope;
    };

    // Stepper changes mutate the *draft*, not the baseline (design-log/045
    // §D). The `change` event still fires for fine-grained subscribers /
    // tests, but the host listens for `apply` and ignores `change`. If the
    // edit happens to land back on the baseline value, the draft self-clears
    // so the Apply bar collapses honestly.
    #onTier(key: keyof RetentionRules, value: number) {
        const next: RetentionRules = { ...this.rules, [key]: value };
        if (this._scope === "remote") {
            this._draftRemote = equalRules(next, normalizeRules(this.remote)) ? null : next;
        } else {
            this._draftLocal = equalRules(next, normalizeRules(this.local)) ? null : next;
        }
        const eff = this.effectiveBoth();
        this.dispatchEvent(
            new CustomEvent<RetentionChangeDetail>("change", {
                detail: eff,
                bubbles: true,
                composed: true,
            }),
        );
    }

    #apply = () => {
        const detail = this.effectiveBoth();
        // Optimistically commit the draft to baseline so the Apply bar collapses
        // immediately. The host re-load after persistence will land on the same
        // values, so willUpdate's null-self-heal keeps drafts at null.
        this.local = detail.local;
        this.remote = detail.remote;
        this._draftLocal = null;
        this._draftRemote = null;
        this.dispatchEvent(
            new CustomEvent<RetentionChangeDetail>("apply", {
                detail,
                bubbles: true,
                composed: true,
            }),
        );
    };

    #discard = () => {
        this._draftLocal = null;
        this._draftRemote = null;
        // Echo a clean `change` so subscribers tracking the "live" rules go
        // back to baseline. The host doesn't need it (it only listens for
        // `apply`) but tests + Storybook find it handy.
        this.dispatchEvent(
            new CustomEvent<RetentionChangeDetail>("change", {
                detail: {
                    local: normalizeRules(this.local),
                    remote: normalizeRules(this.remote),
                },
                bubbles: true,
                composed: true,
            }),
        );
    };
}

declare global {
    interface HTMLElementTagNameMap {
        "retention-rules": RetentionRulesEl;
    }
}
