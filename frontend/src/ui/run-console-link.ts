import { LitElement, css, html } from "lit";
import { customElement } from "lit/decorators.js";
import "./primitives/decoder";

// run-console-link is the RUN-stage clickyclacky that opens the server console
// (design-log/043 Part 2). It mirrors the <run-addresses> row idiom — a quiet,
// full-row button with a faint trailing icon and a decode-in label — but copies
// nothing: activating it emits `press`, which ritual-app turns into ShowLogs.
// The only path to the logs window; there is no global entry (043 Part 1).
@customElement("run-console-link")
export class RunConsoleLink extends LitElement {
    private activate() {
        this.dispatchEvent(new CustomEvent("press", { bubbles: true, composed: true }));
    }

    private onKeyDown(e: KeyboardEvent) {
        if (e.key !== "Enter" && e.key !== " ") return;
        e.preventDefault();
        this.activate();
    }

    render() {
        return html`
            <div
                class="row"
                role="button"
                tabindex="0"
                aria-label="Open server console"
                @click=${() => this.activate()}
                @keydown=${(e: KeyboardEvent) => this.onKeyDown(e)}
            >
                <span class="icon" aria-hidden="true">
                    <svg viewBox="0 0 24 24" width="12" height="12"
                         stroke="currentColor" stroke-width="2"
                         stroke-linecap="round" stroke-linejoin="round" fill="none">
                        <polyline points="4 17 10 11 4 5"></polyline>
                        <line x1="12" y1="19" x2="20" y2="19"></line>
                    </svg>
                </span>
                <span class="label">
                    <rune-decoder
                        .text=${"Server console"}
                        splash-radius="1"
                        splash-tick-ms="22"
                        idle-min-ms="6000"
                        idle-max-ms="14000"
                        idle-radius="1"
                    ></rune-decoder>
                </span>
            </div>
        `;
    }

    static styles = css`
        /* Sized to content (not a full-width row) so it reads as a small,
           secondary transition affordance under the addresses (design-log/043). */
        :host {
            display: block;
            font-size: var(--fs-caption);
            line-height: 14px;
        }
        /* Quiet link, not a pill: no chrome at rest (transparent bg + border),
           faint text. The bevel surface only appears on hover. Run-hue focus
           ring, presses in on activate. */
        .row {
            display: inline-flex;
            align-items: center;
            gap: var(--space-2);
            padding: 3px 8px;
            min-height: 22px;
            border-radius: var(--radius-sm);
            background: transparent;
            border: 1px solid transparent;
            color: var(--text-faint);
            cursor: pointer;
            outline: none;
            user-select: none;
            transition: background var(--motion-fast, 120ms ease),
                        border-color var(--motion-base, 220ms ease),
                        color var(--motion-fast, 120ms ease),
                        transform var(--motion-fast, 120ms ease);
        }
        .row:hover {
            background: var(--stone-bevel);
            border-color: var(--stone-bevel);
            color: var(--text-muted);
        }
        .row:focus-visible {
            box-shadow: 0 0 0 2px color-mix(in srgb, var(--state-run) 60%, transparent);
        }
        .row:active {
            background: var(--stone-edge);
            transform: scale(0.985);
        }
        .icon {
            display: inline-flex;
            align-items: center;
            justify-content: center;
            color: var(--text-faint);
            transition: color var(--motion-base, 220ms ease);
        }
        .row:hover .icon {
            color: var(--state-run);
        }
        .label {
            letter-spacing: 0.02em;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }
        @media (prefers-reduced-motion: reduce) {
            .row { transition: none; }
            .row:active { transform: none; }
        }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        "run-console-link": RunConsoleLink;
    }
}
