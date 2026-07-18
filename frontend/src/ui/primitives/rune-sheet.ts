/**
 * Modal sheet primitive — wraps native <dialog>. Browser provides focus trap,
 * Escape dismiss, and background inert-ness via showModal(). Backdrop click
 * dismiss is added on top (native <dialog> does not dismiss by default).
 *
 * HIG: https://developer.apple.com/design/human-interface-guidelines/sheets
 */

import { LitElement, css, html } from "lit";
import { customElement, property, query, state } from "lit/decorators.js";
import { sharedStyles } from "./_base";

export type RuneSheetDismissReason = "escape" | "backdrop" | "explicit";

export interface RuneSheetCloseDetail {
    reason: RuneSheetDismissReason;
}

@customElement("rune-sheet")
export class RuneSheet extends LitElement {
    @property({ type: Boolean, reflect: true }) open = false;
    @property() heading = "";

    @query("dialog") private _dialog!: HTMLDialogElement;
    private _lastReason: RuneSheetDismissReason = "explicit";
    // Slot occupancy flags, kept reactive via @slotchange so render stays a
    // pure function of state and doesn't walk light-DOM children —
    // design-log/020 §G.
    @state() private _hasHeaderSlot = false;
    @state() private _hasFooterSlot = false;

    static styles = [
        ...sharedStyles,
        css`
            :host { display: contents; }

            dialog {
                margin: auto;
                padding: 0;
                border: none;
                background: var(--surface-floating);
                color: var(--text);
                border-radius: var(--radius-lg);
                box-shadow:
                    0 24px 64px rgba(0, 0, 0, 0.6),
                    inset 0 1px 0 var(--stone-bevel);
                max-width: min(520px, 92vw);
                width: 100%;
                font-family: var(--font-mono);
            }
            dialog::backdrop {
                background: var(--surface-overlay);
            }
            dialog:not([open]) { display: none; }

            header {
                padding: var(--space-4) var(--space-5) var(--space-3);
                font-size: var(--fs-title);
                color: var(--text-strong);
                letter-spacing: 0.02em;
                border-bottom: 1px solid var(--stone-bevel);
            }

            .body {
                padding: var(--space-4) var(--space-5);
            }

            footer {
                display: flex;
                justify-content: flex-end;
                gap: var(--space-2);
                padding: var(--space-3) var(--space-5) var(--space-4);
                border-top: 1px solid var(--stone-bevel);
            }
            footer:empty { display: none; }
        `,
    ];

    disconnectedCallback() {
        super.disconnectedCallback();
        // Close the modal so leftover modal stack frames don't leak between
        // test fixtures or screen swaps.
        if (this._dialog?.open) this._dialog.close();
    }

    updated(changed: Map<string, unknown>) {
        if (!changed.has("open") || !this._dialog) return;
        if (this.open && !this._dialog.open) {
            try {
                this._dialog.showModal();
            } catch {
                // Fallback to non-modal open if showModal is unavailable in
                // the host context (e.g. another modal is already on the
                // top-layer stack, or detached frames).
                this._dialog.open = true;
            }
            this.dispatchEvent(new CustomEvent("open", { bubbles: true, composed: true }));
        } else if (!this.open && this._dialog.open) {
            this._dialog.close();
            // Synthesise the close lifecycle: native `close` is also dispatched
            // by the dialog, but we want a single deterministic path that
            // carries our `reason` detail and is host-context-independent.
            this.#onClose();
        }
    }

    render() {
        const showHeader = !!(this.heading || this._hasHeaderSlot);
        return html`
            <dialog
                @cancel=${this.#onCancel}
                @click=${this.#onBackdropClick}
                part="dialog"
            >
                <header part="header" ?hidden=${!showHeader}>
                    <slot name="header" @slotchange=${this.#onHeaderSlot}>${this.heading}</slot>
                </header>
                <div class="body" part="body">
                    <slot></slot>
                </div>
                <footer part="footer" ?hidden=${!this._hasFooterSlot}>
                    <slot name="footer" @slotchange=${this.#onFooterSlot}></slot>
                </footer>
            </dialog>
        `;
    }

    /** Programmatic open. */
    show() {
        this._lastReason = "explicit";
        this.open = true;
    }

    /** Programmatic close. */
    close(reason: RuneSheetDismissReason = "explicit") {
        this._lastReason = reason;
        this.open = false;
    }

    #onCancel = (e: Event) => {
        e.preventDefault();
        this._lastReason = "escape";
        this.open = false;
    };

    #onBackdropClick = (e: MouseEvent) => {
        if (e.target !== this._dialog) return;
        this._lastReason = "backdrop";
        this.open = false;
    };

    #onClose = () => {
        this.open = false;
        const detail: RuneSheetCloseDetail = { reason: this._lastReason };
        this.dispatchEvent(new CustomEvent("close", {
            bubbles: true,
            composed: true,
            detail,
        }));
        this.dispatchEvent(new CustomEvent("dismiss", {
            bubbles: true,
            composed: true,
            detail,
        }));
        this._lastReason = "explicit";
    };

    #onHeaderSlot = (e: Event) => {
        const slot = e.target as HTMLSlotElement;
        this._hasHeaderSlot = slot.assignedElements().length > 0;
    };

    #onFooterSlot = (e: Event) => {
        const slot = e.target as HTMLSlotElement;
        this._hasFooterSlot = slot.assignedElements().length > 0;
    };
}

declare global {
    interface HTMLElementTagNameMap {
        "rune-sheet": RuneSheet;
    }
}
