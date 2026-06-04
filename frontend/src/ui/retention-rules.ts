/**
 * Retention rules — editable Borg-style tier picker that EXPLAINS the policy
 * (design-log/033; wired by /039; redesigned 2026-06-04). It is **not** a dry-run
 * over the user's real backups and never implies their local backups will be
 * deleted.
 *
 * The calendar cascade IS the control: one lane per tier over a shared
 * recent→older time axis with month labels; each lane carries its own uncapped
 * `rune-stepper` and shows representative kept dates as dots positioned by date.
 * No separate stepper block, minimal prose. A Local·R2 scope switch edits both
 * sides; a keep_last:0 caution remains.
 *
 * Presentational + pure: holds both sides' `rules`, emits `change` {local,
 * remote} on any edit. `now` is a property (default fixed) so render() reads no
 * wall-clock (design-log/020); the dates are illustrative + universal.
 */

import { LitElement, css, html } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import "./primitives/rune-segmented";
import "./primitives/rune-stepper";
import type { RetentionRules, Tier } from "./retention-model";

export type RetentionScope = "local" | "remote";

export interface RetentionChangeDetail {
    local: RetentionRules;
    remote: RetentionRules;
}

const FIXED_NOW = new Date("2026-06-04T12:00:00Z");
const VISIBLE = 8; // dots drawn per lane; the count itself is uncapped (+N overflow)
const MONTH_FMT = new Intl.DateTimeFormat(undefined, { month: "short" });
const DAY = 24 * 3600 * 1000;

const TIERS: { key: keyof RetentionRules; label: string; tier: Tier; unit: string }[] = [
    { key: "keepLast", label: "Keep last", tier: "last", unit: "" },
    { key: "keepDaily", label: "Keep daily", tier: "daily", unit: "day" },
    { key: "keepWeekly", label: "Keep weekly", tier: "weekly", unit: "week" },
    { key: "keepMonthly", label: "Keep monthly", tier: "monthly", unit: "month" },
];

// Tier → hue, reusing the dial state palette (design-log/033 §Q5).
const TIER_HUE: Record<Tier, string> = {
    last: "var(--state-run, #3fb950)",
    daily: "var(--state-prep, #d29922)",
    weekly: "var(--state-idle, #58a6ff)",
    monthly: "var(--state-final, #a371f7)",
};

const SCOPE_OPTS = [
    { value: "local", label: "Local" },
    { value: "remote", label: "R2" },
];

const DEFAULT_RULES: RetentionRules = { keepLast: 2, keepDaily: 0, keepWeekly: 0, keepMonthly: 0 };

const plural = (n: number, unit: string) => `${n} ${unit}${n === 1 ? "" : "s"}`;

// describePolicy → one plain sentence; used as the a11y label so screen readers
// get the full meaning while the visual stays terse (design-log/033 §Redesign).
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

// stepBack — the date `i` cadence-steps before now for a tier. Illustrative +
// universal (not the user's real backups). last + daily share a daily cadence.
function stepBack(now: Date, tier: Tier, i: number): Date {
    const d = new Date(now);
    if (tier === "monthly") d.setUTCMonth(d.getUTCMonth() - i);
    else if (tier === "weekly") d.setUTCDate(d.getUTCDate() - i * 7);
    else d.setUTCDate(d.getUTCDate() - i);
    return d;
}

@customElement("retention-rules")
export class RetentionRulesEl extends LitElement {
    @property({ attribute: false }) local: RetentionRules = { ...DEFAULT_RULES };
    @property({ attribute: false }) remote: RetentionRules = { ...DEFAULT_RULES };
    @property({ attribute: false }) now: Date = FIXED_NOW;

    @state() private _scope: RetentionScope = "local";

    static styles = css`
        :host {
            display: block;
            font-family: var(--font-mono);
            color: var(--text);
        }
        .switch {
            margin-bottom: var(--space-4);
        }
        .cascade {
            display: flex;
            flex-direction: column;
            gap: var(--space-2);
        }
        /* label | stepper | time track — columns align across the axis + lanes */
        .lane,
        .axis {
            display: grid;
            grid-template-columns: 96px 84px 1fr;
            align-items: center;
            gap: var(--space-2);
        }
        .lane-label {
            color: var(--text-muted);
            white-space: nowrap;
        }
        .lane[data-zero] .lane-label {
            color: var(--text-faint);
        }
        .track {
            position: relative;
            height: 16px;
        }
        .dot {
            position: absolute;
            top: 50%;
            width: 8px;
            height: 8px;
            border-radius: 50%;
            transform: translate(-50%, -50%);
        }
        .more {
            position: absolute;
            top: 50%;
            right: 0;
            transform: translateY(-50%);
            font-size: var(--fs-caption);
            color: var(--text-faint);
        }
        /* Time axis: month labels (the "calendar"), recent on the left. */
        .axis {
            margin-bottom: var(--space-1);
            font-size: var(--fs-caption);
            color: var(--text-faint);
        }
        .axis .track {
            height: 1.2em;
        }
        .ends {
            display: flex;
            justify-content: space-between;
        }
        .month {
            position: absolute;
            top: 0;
            transform: translateX(-50%);
            white-space: nowrap;
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
    `;

    private get rules(): RetentionRules {
        return this._scope === "remote" ? this.remote : this.local;
    }

    render() {
        const rules = this.rules;
        const now = this.now.getTime();

        // Shared time window = from now back to the oldest date any tier reaches.
        let oldest = now;
        for (const t of TIERS) {
            const n = rules[t.key];
            if (n > 0) oldest = Math.min(oldest, stepBack(this.now, t.tier, n - 1).getTime());
        }
        const span = Math.max(DAY, now - oldest);
        const pos = (ms: number) => `${Math.max(0, Math.min(100, ((now - ms) / span) * 100))}%`;

        return html`
            <div class="switch">
                <rune-segmented
                    .options=${SCOPE_OPTS}
                    value=${this._scope}
                    label="Scope"
                    @change=${this.#onScope}
                ></rune-segmented>
            </div>
            <div class="cascade" role="group" aria-label=${describePolicy(rules)}>
                <div class="axis">
                    <span></span>
                    <span class="ends"><span>recent</span></span>
                    <div class="track">${this.#months(oldest, now, pos)}</div>
                </div>
                ${TIERS.map((t) => this.#renderLane(t, pos))}
            </div>
            ${rules.keepLast === 0 ? this.#renderCaution() : null}
        `;
    }

    #months(oldest: number, now: number, pos: (ms: number) => string) {
        const labels: { label: string; time: number }[] = [];
        const d = new Date(now);
        d.setUTCDate(1);
        d.setUTCHours(0, 0, 0, 0);
        while (d.getTime() >= oldest && labels.length < 7) {
            labels.push({ label: MONTH_FMT.format(d), time: d.getTime() });
            d.setUTCMonth(d.getUTCMonth() - 1);
        }
        return labels.map((m) => html`<span class="month" style=${`left:${pos(m.time)}`}>${m.label}</span>`);
    }

    #renderLane(t: { key: keyof RetentionRules; label: string; tier: Tier }, pos: (ms: number) => string) {
        const count = this.rules[t.key];
        const shown = Math.min(count, VISIBLE);
        const dots = Array.from({ length: shown }, (_, i) => {
            const d = stepBack(this.now, t.tier, i);
            return html`<i
                class="dot"
                style=${`left:${pos(d.getTime())};background:${TIER_HUE[t.tier]}`}
                title=${d.toISOString().slice(0, 10)}
            ></i>`;
        });
        return html`<div class="lane" data-tier=${t.label} ?data-zero=${count === 0}>
            <span class="lane-label">${t.label}</span>
            <rune-stepper
                .value=${count}
                min="0"
                .max=${Infinity}
                label=${t.label}
                @change=${(e: CustomEvent<{ value: number }>) => this.#onTier(t.key, e.detail.value)}
            ></rune-stepper>
            <div class="track">
                ${dots} ${count > VISIBLE ? html`<span class="more">+${count - VISIBLE}</span>` : null}
            </div>
        </div>`;
    }

    #renderCaution() {
        return html`<p class="caution">
            Without "keep last", the most recent backup isn't guaranteed to be kept.
        </p>`;
    }

    #onScope = (e: CustomEvent<{ value: string }>) => {
        this._scope = e.detail.value as RetentionScope;
    };

    #onTier(key: keyof RetentionRules, value: number) {
        const next: RetentionRules = { ...this.rules, [key]: value };
        if (this._scope === "remote") this.remote = next;
        else this.local = next;
        this.dispatchEvent(
            new CustomEvent<RetentionChangeDetail>("change", {
                detail: { local: this.local, remote: this.remote },
                bubbles: true,
                composed: true,
            }),
        );
    }
}

declare global {
    interface HTMLElementTagNameMap {
        "retention-rules": RetentionRulesEl;
    }
}
