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
import { customElement, property } from "lit/decorators.js";
import "./prep-settings";
import "./sync-view";
import "./primitives/rune-button";
import type { PrepSettings } from "./prep-settings";
import type { SyncVerdict } from "./sync-view";

@customElement("advanced-view")
export class AdvancedView extends LitElement {
    @property({ attribute: false }) config: PrepSettings = { port: 25565, memoryMB: 4096 };
    @property({ attribute: false }) check: () => Promise<SyncVerdict> = async () => ({
        behind: false,
        ahead: false,
    });
    // Gates the manual "Check for update" — the flow restarts the process, so
    // it is offered only when the dial is idle (design-log/037 §Q4 lean).
    @property({ type: Boolean }) canUpdate = false;

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
                <prep-settings .config=${this.config}></prep-settings>
            </section>
            <section>
                <p class="label">Sync</p>
                <sync-view auto .check=${this.check}></sync-view>
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
}

declare global {
    interface HTMLElementTagNameMap {
        "advanced-view": AdvancedView;
    }
}
