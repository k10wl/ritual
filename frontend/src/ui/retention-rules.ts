/**
 * Retention rules — the editable Borg-style tier picker with a live kept-vs-
 * pruned visualization (design-log/033, wired end-to-end by /039). One component
 * edits both sides via a Local·R2 scope switch (§Q6): four `rune-segmented`
 * tier controls (0–5) + a computed summary, legend, timeline, and a keep_last:0
 * caution. The picture is computed by the real `mark()` union over the selected
 * scope's history — never faked.
 *
 * Presentational + pure: holds both sides' `rules`, emits `change` {local,
 * remote} on any edit (the host persists both via setRetentionRules). `now` and
 * `backups` are properties (default a fixed now + synthetic sample) so render()
 * reads no wall-clock (design-log/020).
 *
 * HIG: https://developer.apple.com/design/human-interface-guidelines/segmented-controls
 */

import { LitElement, css, html } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import "./primitives/rune-segmented";
import {
    mark,
    sample,
    summarize,
    monthKey,
    type Backup,
    type Marked,
    type RetentionRules,
    type Tier,
} from "./retention-model";

export type RetentionScope = "local" | "remote";

export interface RetentionChangeDetail {
    local: RetentionRules;
    remote: RetentionRules;
}

const FIXED_NOW = new Date("2026-06-04T12:00:00Z");

const TIERS: { key: keyof RetentionRules; label: string; tier: Tier }[] = [
    { key: "keepLast", label: "Keep last", tier: "last" },
    { key: "keepDaily", label: "Keep daily", tier: "daily" },
    { key: "keepWeekly", label: "Keep weekly", tier: "weekly" },
    { key: "keepMonthly", label: "Keep monthly", tier: "monthly" },
];

// Tier → hue, reusing the dial state palette (design-log/033 §Q5).
const TIER_HUE: Record<Tier, string> = {
    last: "var(--state-run, #3fb950)",
    daily: "var(--state-prep, #d29922)",
    weekly: "var(--state-idle, #58a6ff)",
    monthly: "var(--state-final, #a371f7)",
};

const TIER_OPTS = ["0", "1", "2", "3", "4", "5"].map((v) => ({ value: v, label: v }));
const SCOPE_OPTS = [
    { value: "local", label: "Local" },
    { value: "remote", label: "R2" },
];

const DEFAULT_RULES: RetentionRules = { keepLast: 2, keepDaily: 0, keepWeekly: 0, keepMonthly: 0 };

@customElement("retention-rules")
export class RetentionRulesEl extends LitElement {
    @property({ attribute: false }) local: RetentionRules = { ...DEFAULT_RULES };
    @property({ attribute: false }) remote: RetentionRules = { ...DEFAULT_RULES };
    /** Per-scope history feeding the timeline; null → synthetic sample (§Q5). */
    @property({ attribute: false }) localBackups: Backup[] | null = null;
    @property({ attribute: false }) remoteBackups: Backup[] | null = null;
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
        .tiers {
            display: flex;
            flex-direction: column;
            gap: var(--space-2);
        }
        .tier {
            display: grid;
            grid-template-columns: 1fr auto;
            align-items: center;
            gap: var(--space-3);
        }
        .tier-label {
            color: var(--text-muted);
        }
        .summary {
            margin: var(--space-4) 0 var(--space-2);
            color: var(--text-strong);
        }
        .legend {
            display: flex;
            flex-wrap: wrap;
            gap: var(--space-3);
            font-size: var(--fs-caption);
            color: var(--text-muted);
        }
        .legend span {
            display: inline-flex;
            align-items: center;
            gap: var(--space-1);
        }
        .dot {
            width: 0.7em;
            height: 0.7em;
            border-radius: 50%;
            display: inline-block;
        }
        .dot.hollow {
            border: 1px solid var(--text-faint);
        }
        .timeline {
            position: relative;
            height: 56px;
            margin-top: var(--space-3);
            border-top: 1px solid var(--stone-bevel);
        }
        .month {
            position: absolute;
            top: 2px;
            font-size: var(--fs-caption);
            color: var(--text-faint);
            transform: translateX(-50%);
        }
        .tick {
            position: absolute;
            bottom: 6px;
            width: 7px;
            height: 7px;
            border-radius: 50%;
            transform: translateX(-50%);
        }
        .tick.pruned {
            width: 4px;
            height: 4px;
            background: transparent;
            border: 1px solid var(--text-faint);
            bottom: 8px;
        }
        .caution {
            margin-top: var(--space-3);
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

    private get backups(): Backup[] {
        const explicit = this._scope === "remote" ? this.remoteBackups : this.localBackups;
        return explicit ?? sample(this.now);
    }

    render() {
        const marked = mark(this.backups, this.rules);
        const sum = summarize(marked);
        return html`
            <div class="switch">
                <rune-segmented
                    .options=${SCOPE_OPTS}
                    value=${this._scope}
                    label="Scope"
                    @change=${this.#onScope}
                ></rune-segmented>
            </div>
            <div class="tiers">
                ${TIERS.map((t) => this.#renderTier(t))}
            </div>
            <p class="summary">
                Keeping ${sum.kept} of ${sum.total} backups · ${sum.spanDays} days of history
            </p>
            ${this.#renderLegend(marked)}
            ${this.#renderTimeline(marked)}
            ${this.rules.keepLast === 0 ? this.#renderCaution() : null}
        `;
    }

    #renderTier(t: { key: keyof RetentionRules; label: string }) {
        return html`<div class="tier">
            <span class="tier-label">${t.label}</span>
            <rune-segmented
                .options=${TIER_OPTS}
                value=${String(this.rules[t.key])}
                label=${t.label}
                @change=${(e: CustomEvent<{ value: string }>) => this.#onTier(t.key, e.detail.value)}
            ></rune-segmented>
        </div>`;
    }

    #renderLegend(marked: Marked[]) {
        const counts: Record<Tier, number> = { last: 0, daily: 0, weekly: 0, monthly: 0 };
        let pruned = 0;
        for (const m of marked) {
            if (!m.kept) pruned++;
            for (const tier of m.tiers) counts[tier]++;
        }
        return html`<div class="legend">
            ${TIERS.map(
                (t) => html`<span
                    ><i class="dot" style=${`background:${TIER_HUE[t.tier]}`}></i> ${t.tier} ${counts[t.tier]}</span
                >`,
            )}
            <span><i class="dot hollow"></i> pruned ${pruned}</span>
        </div>`;
    }

    #renderTimeline(marked: Marked[]) {
        if (marked.length === 0) return null;
        const newest = marked[0].date.getTime();
        const oldest = marked[marked.length - 1].date.getTime();
        const span = Math.max(1, newest - oldest);
        const pos = (t: number) => `${((t - oldest) / span) * 100}%`;

        // Month ruler labels at each distinct UTC month boundary present.
        const months = new Map<string, number>();
        for (const m of marked) {
            const k = monthKey(m.date);
            if (!months.has(k)) months.set(k, m.date.getTime());
        }

        return html`<div class="timeline" role="img" aria-label="Backup retention timeline">
            ${[...months.entries()].map(
                ([k, t]) => html`<span class="month" style=${`left:${pos(t)}`}>${k.slice(5)}</span>`,
            )}
            ${marked.map((m) => {
                const hue = m.kept ? TIER_HUE[m.tiers[0]] : "transparent";
                return html`<i
                    class="tick ${m.kept ? "" : "pruned"}"
                    style=${`left:${pos(m.date.getTime())};background:${hue}`}
                    title=${m.id}
                ></i>`;
            })}
        </div>`;
    }

    #renderCaution() {
        return html`<p class="caution">
            Without "keep last", the newest backup can be pruned after the next session.
        </p>`;
    }

    #onScope = (e: CustomEvent<{ value: string }>) => {
        this._scope = e.detail.value as RetentionScope;
    };

    #onTier(key: keyof RetentionRules, value: string) {
        const n = Number.parseInt(value, 10);
        const next: RetentionRules = { ...this.rules, [key]: n };
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
