/**
 * Navigation-stack context (design-log/034). The `rune-stack` primitive
 * provides a `NavController` so any descendant — at any depth — can push a
 * deeper view or pop back without prop-drilling.
 *
 * A view is a plain descriptor, not a component subclass: it carries a stable
 * `id`, an optional back-bar `title`, and a `render(nav)` that returns
 * arbitrary content and receives the controller so nested selections can push
 * further.
 */

import { createContext } from "@lit/context";
import type { TemplateResult } from "lit";

export interface NavController {
    /** Push a view; the screen translates to it. */
    push(view: NavView): void;
    /** Pop the top view; the screen translates back one level. No-op at root. */
    pop(): void;
    /** Pop every pushed view back to the root. */
    popToRoot(): void;
    /** Number of pushed views above the root (0 at root). */
    readonly depth: number;
}

export interface NavView {
    /** Stable key for the pane. */
    id: string;
    /** Back-bar label for this pane; omit for a blank bar. */
    title?: string;
    /** Pane body; receives the controller so nested rows can push deeper. */
    render: (nav: NavController) => TemplateResult;
}

export const navContext = createContext<NavController>(Symbol("rune-nav"));
