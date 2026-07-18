/**
 * Navigation-stack primitive (design-log/034). The root view is the default
 * slot; pushing a view slides the whole screen left so the new pane enters
 * from the right, popping slides it back out. Arbitrary depth, lazy-mounted
 * panes, a stack-owned `←` back bar, and one continuous transform — the
 * pleasing, modal-free alternative to dialogs / popups / toasts.
 *
 * All live panes sit in a non-wrapping flex row; the track is shifted
 * `translateX(-index·100%)`. Descendants reach the controller through
 * `navContext` (no prop-drilling); each view's `render(nav)` also receives it
 * so nested selections can push deeper.
 *
 * HIG — Navigation (hierarchical push/pop):
 * https://developer.apple.com/design/human-interface-guidelines/navigation-and-search
 */

import { LitElement, css, html } from "lit";
import { customElement, state } from "lit/decorators.js";
import { repeat } from "lit/directives/repeat.js";
import { ContextProvider } from "@lit/context";
import { sharedStyles } from "./_base";
import { navContext, type NavController, type NavView } from "../contexts/nav-context";
import "./rune-button";

const reducedMotion = () =>
    window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;

@customElement("rune-stack")
export class RuneStack extends LitElement implements NavController {
    // Pushed views above the root (root = default slot, pane index 0). Panes
    // beyond `_index` are mid-leave: kept in the DOM so they slide out, trimmed
    // (unmounted) once the transform settles — design-log/034 §Q2.
    @state() private _views: NavView[] = [];
    // Active pane index = how deep we are. Drives translateX(-_index·100%) and
    // is the public `depth`.
    @state() private _index = 0;

    constructor() {
        super();
        // Provide the controller to every descendant; `this` implements it and
        // never changes, so the provider needs no later setValue. It registers
        // itself as a reactive controller on `this`, so no reference is kept.
        new ContextProvider(this, { context: navContext, initialValue: this });
    }

    connectedCallback() {
        super.connectedCallback();
        // Keyboard back: Esc and ← pop one level (design-log/034 req. 8). A
        // window listener so it works regardless of focus; only acts above the
        // root, and ← yields to text fields so it can't steal the caret.
        window.addEventListener("keydown", this.#onKeydown);
    }

    disconnectedCallback() {
        window.removeEventListener("keydown", this.#onKeydown);
        super.disconnectedCallback();
    }

    #onKeydown = (e: KeyboardEvent) => {
        if (this._index === 0 || e.defaultPrevented) return;
        if (e.key === "Escape") {
            e.preventDefault();
            this.pop();
        } else if (e.key === "ArrowLeft" && !this.#inEditable(e)) {
            e.preventDefault();
            this.pop();
        }
    };

    // True when the keystroke originated in an editable control (input /
    // textarea / select / contenteditable), across shadow boundaries — there
    // ← must move the caret, not navigate.
    #inEditable(e: KeyboardEvent): boolean {
        const t = e.composedPath()[0] as HTMLElement | undefined;
        if (!t || !t.tagName) return false;
        return (
            t.isContentEditable ||
            t.tagName === "INPUT" ||
            t.tagName === "TEXTAREA" ||
            t.tagName === "SELECT"
        );
    }

    static styles = [
        ...sharedStyles,
        css`
            :host {
                display: block;
                position: relative;
                overflow: hidden;
                height: 100%;
                font-family: var(--font-mono);
            }

            .track {
                display: flex;
                flex-wrap: nowrap;
                height: 100%;
                transform: translateX(calc(var(--i, 0) * -100%));
                transition: transform var(--rune-stack-motion, 360ms cubic-bezier(.2, 0, .0, 1));
                will-change: transform;
            }
            @media (prefers-reduced-motion: reduce) {
                .track { transition: none; }
            }

            .pane {
                flex: 0 0 100%;
                min-width: 0;
                height: 100%;
                overflow: auto;
                display: flex;
                flex-direction: column;
                /* Opaque so a pushed pane fully covers the one it slides over. */
                background: var(--stone-deep);
            }

            /* Ambient back affordance — no chrome, no separating border. Reads
               like the quiet footer links, not an OS nav bar. */
            .bar {
                display: flex;
                align-items: center;
                gap: var(--space-2);
                padding: var(--space-3) var(--space-4) var(--space-2);
                flex: 0 0 auto;
            }
            .bar .title {
                color: var(--text-faint);
                font-size: var(--fs-caption);
                letter-spacing: 0.08em;
                text-transform: lowercase;
            }
            .back {
                --rune-button-fg: var(--text-faint);
                --rune-button-padding: 0 var(--space-1);
                font-size: var(--fs-body);
                line-height: 1;
            }

            .body {
                flex: 1 1 auto;
                min-height: 0;
                overflow: auto;
            }
        `,
    ];

    /** Pushed-view count above the root (0 at root). */
    get depth(): number {
        return this._index;
    }

    push = (view: NavView): void => {
        this._views = [...this._views, view];
        this._index = this._views.length;
        this.#emit();
    };

    pop = (): void => {
        if (this._index === 0) return;
        this._index -= 1;
        this.#emit();
        this.#scheduleTrim();
    };

    popToRoot = (): void => {
        if (this._index === 0) return;
        this._index = 0;
        this.#emit();
        this.#scheduleTrim();
    };

    render() {
        return html`
            <div
                class="track"
                style=${`--i:${this._index}`}
                @transitionend=${this.#onTransitionEnd}
            >
                <section class="pane" part="pane">
                    <slot></slot>
                </section>
                ${repeat(
                    this._views,
                    (v) => v.id,
                    (v) => html`
                        <section class="pane" part="pane" data-view=${v.id}>
                            <div class="bar" part="bar">
                                <rune-button
                                    class="back"
                                    variant="plain"
                                    size="sm"
                                    aria-label="Back"
                                    @press=${this.pop}
                                >←</rune-button>
                                <span class="title">${v.title ?? ""}</span>
                            </div>
                            <div class="body">${v.render(this)}</div>
                        </section>
                    `,
                )}
            </div>
        `;
    }

    // Trim offscreen-right panes once the slide settles. Under reduced motion
    // no transform transition fires, so settle on the next frame instead.
    #scheduleTrim() {
        if (reducedMotion()) {
            void this.updateComplete.then(() => this.#trim());
        }
    }

    #onTransitionEnd = (e: TransitionEvent) => {
        if (e.propertyName !== "transform") return;
        this.#trim();
    };

    #trim() {
        if (this._views.length > this._index) {
            this._views = this._views.slice(0, this._index);
        }
    }

    #emit() {
        // Any transition releases focus: the focused control may be in the pane
        // that's leaving (e.g. Esc from a Settings field), and stranded focus on
        // an off-screen / unmounting element is a trap. Blur the truly-active
        // element across shadow boundaries.
        this.#blurActive();
        this.dispatchEvent(
            new CustomEvent("navigate", {
                detail: { depth: this._index },
                bubbles: true,
                composed: true,
            }),
        );
    }

    #blurActive() {
        let el: Element | null = document.activeElement;
        while (el?.shadowRoot?.activeElement) el = el.shadowRoot.activeElement;
        (el as HTMLElement | null)?.blur?.();
    }
}

declare global {
    interface HTMLElementTagNameMap {
        "rune-stack": RuneStack;
    }
}
