/**
 * Advanced view — the single staged pane behind the IDLE "advanced" link
 * (design-log/034). Two flat sections, no nesting: **Server** (port + memory,
 * ex-inline disclosure 014) and **Sync** (031 Download / Upload).
 *
 * Presentational + structural: it lays out the two sections and passes inputs
 * down (`config` → prep-settings, `check` → sync-view). The children's events
 * (`change`, `sync`) bubble straight through to the host — this element adds no
 * wiring of its own.
 */

import { LitElement, css, html } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import "./prep-settings";
import "./sync-view";
import "./versions-view";
import "./retention-rules";
import "./primitives/rune-button";
import type { PrepSettings } from "./prep-settings";
import type { SyncVerdict } from "./sync-view";
import type { VersionRow, VersionScope, LocalStorageStatsLike } from "./versions-view";
import type { RetentionChangeDetail } from "./retention-rules";
import type { RetentionRules } from "./retention-model";

const DEFAULT_RULES: RetentionRules = { keepLast: 2, keepDaily: 0, keepWeekly: 0, keepMonthly: 0 };
type RetentionPair = { local: RetentionRules; remote: RetentionRules };

@customElement("advanced-view")
export class AdvancedView extends LitElement {
    @property({ attribute: false }) config: PrepSettings = { port: 25565, memoryMB: 4096 };
    // Transient skip-sync toggle (design-log/036 §Q6), host-owned per [[044]].
    // Forwarded down so a re-mounted <prep-settings> (lazy nav stack, [[034]])
    // reflects the session's current value instead of resetting to OFF.
    @property({ type: Boolean }) skipSync = false;
    @property({ attribute: false }) check: () => Promise<SyncVerdict> = async () => ({
        behind: false,
        ahead: false,
    });
    // Version listing for the rollback section (design-log/038), now scope-
    // aware so the tab switch (design-log/045 §B) can re-run with Local /
    // Remote. `dirty` surfaces the "Publish first" nudge inside the Restore
    // confirm. `versionStats` feeds the on-disk header (Local tab only,
    // design-log/045 §E).
    @property({ attribute: false }) versions: (scope: VersionScope) => Promise<VersionRow[]> = async () => [];
    @property({ attribute: false }) versionStats: () => Promise<LocalStorageStatsLike> = async () => ({
        bytesOnDisk: 0,
        objectCount: 0,
    });
    @property({ type: Boolean }) dirty = false;
    // Retention rules section (design-log/039), injected by the host: loadRules
    // wraps getRetentionRules (snake→camel mapped host-side). The preview is
    // illustrative (it explains the policy, not the user's real backups — /033
    // §Redesign), so no backup history is fed in. Edits re-emit as
    // `retentionchange` (a distinct name so they don't collide with
    // prep-settings `change`).
    @property({ attribute: false }) loadRules: () => Promise<RetentionPair> = async () => ({
        local: { ...DEFAULT_RULES },
        remote: { ...DEFAULT_RULES },
    });
    // Gates the manual "Check for update" — the flow restarts the process, so
    // it is offered only when the dial is idle (design-log/037 §Q4 lean).
    @property({ type: Boolean }) canUpdate = false;

    @state() private _rules: RetentionPair = { local: { ...DEFAULT_RULES }, remote: { ...DEFAULT_RULES } };

    firstUpdated() {
        // Lazy remount per Advanced navigation (design-log/034) → load the
        // current rules on each open so the picker is never stale.
        void this.loadRules().then((r) => (this._rules = r));
    }

    static styles = css`
        :host {
            display: block;
            font-family: var(--font-mono);
        }
        section {
            padding: var(--space-4);
        }
        section + section {
            border-top: 1px solid var(--stone-bevel);
        }
        .label {
            margin: 0 0 var(--space-3);
            color: var(--text-faint);
            font-size: var(--fs-caption);
            font-weight: 400;
            letter-spacing: 0.12em;
            text-transform: uppercase;
        }
    `;

    render() {
        return html`
            <section>
                <p class="label">Server</p>
                <prep-settings .config=${this.config} ?skipSync=${this.skipSync}></prep-settings>
            </section>
            <section>
                <p class="label">Sync</p>
                <sync-view auto .check=${this.check}></sync-view>
            </section>
            <section>
                <p class="label">Versions</p>
                <versions-view
                    .list=${this.versions}
                    .stats=${this.versionStats}
                    ?dirty=${this.dirty}
                ></versions-view>
            </section>
            <section>
                <p class="label">Retention</p>
                <retention-rules
                    .local=${this._rules.local}
                    .remote=${this._rules.remote}
                    @change=${this.#onRetentionChange}
                    @apply=${this.#onRetentionApply}
                ></retention-rules>
            </section>
            <section>
                <p class="label">Updates</p>
                <rune-button
                    variant="tinted"
                    ?disabled=${!this.canUpdate}
                    @press=${this.emitCheckUpdate}
                >Check for update</rune-button>
            </section>
        `;
    }

    // Re-emit the button press as a domain event the host wires to the
    // autoupdate flow (design-log/037 §Q6). Presentational rule: behavior
    // exits via a custom event, not a wails-api call here.
    private emitCheckUpdate = () => {
        this.dispatchEvent(new CustomEvent("checkupdate", { bubbles: true, composed: true }));
    };

    // Retention edits arrive as a generic `change` from `<retention-rules>`.
    // Under the staged-edit model (design-log/045 §D) this fires on every
    // stepper tap but does NOT auto-save; the host listens for `apply`
    // instead.
    //
    // Crucially we must NOT write the draft back into `this._rules`: that field
    // is the *baseline* we hand down as `.local` / `.remote`, and the child
    // treats it as "what's saved right now". Echoing the live draft into it
    // (a) makes the child's `willUpdate` self-heal clear the draft, collapsing
    // the Apply bar after a single tap, and (b) lets a stray composed event
    // overwrite a good baseline with garbage. `_rules` only changes on load
    // (firstUpdated) and on Apply (#onRetentionApply). Here we just re-emit as
    // `retentionchange` for external subscribers and stop the generic `change`
    // so it can't be mistaken for the prep-settings `change` the host listens to.
    #onRetentionChange = (e: Event) => {
        e.stopPropagation();
        const detail = (e as CustomEvent<RetentionChangeDetail>).detail;
        this.dispatchEvent(
            new CustomEvent<RetentionChangeDetail>("retentionchange", {
                detail,
                bubbles: true,
                composed: true,
            }),
        );
    };

    // Retention Apply (design-log/045 §D): the user pressed Apply on the
    // staged-edit bar. Stop the inner `apply` so it doesn't collide with any
    // unrelated `apply` events bubbling through the navigation stack, and
    // re-emit as the distinct `retentionapply` the host handles
    // (setRetentionRules + applyRetentionNow). Carries the {local, remote}
    // payload the host needs to persist.
    #onRetentionApply = (e: Event) => {
        e.stopPropagation();
        const detail = (e as CustomEvent<RetentionChangeDetail>).detail;
        this._rules = { local: detail.local, remote: detail.remote };
        this.dispatchEvent(
            new CustomEvent<RetentionChangeDetail>("retentionapply", {
                detail,
                bubbles: true,
                composed: true,
            }),
        );
    };
}

declare global {
    interface HTMLElementTagNameMap {
        "advanced-view": AdvancedView;
    }
}
