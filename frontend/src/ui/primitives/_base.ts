/* Shared base for every rune-* primitive. Adopts the cross-engine reset
   into every shadow root; tokens reach primitives via :host inheritance
   from the document :root, so no token import is needed here.

   The reset CSS is inlined here as a tagged template so both Vite (build)
   and @web/test-runner (esbuild plugin) consume it without needing the
   Vite-specific `?inline` query suffix. Keep the source-of-truth shape in
   sync with `public/reset.css` if you ever need to ship the reset to a
   non-shadow consumer. */

import { CSSResult, css } from "lit";

const reset = css`
    @layer reset {
        *, *::before, *::after { box-sizing: border-box; }
        * { margin: 0; }

        :host { display: block; }

        button, input, select, textarea {
            font: inherit;
            color: inherit;
            -webkit-appearance: none;
            appearance: none;
            background: none;
            border: none;
            padding: 0;
        }

        input[type="number"]::-webkit-inner-spin-button,
        input[type="number"]::-webkit-outer-spin-button {
            -webkit-appearance: none;
            margin: 0;
        }
        input[type="number"] {
            -moz-appearance: textfield;
        }

        * {
            user-select: none;
            -webkit-user-select: none;
        }
        input, textarea {
            user-select: text;
            -webkit-user-select: text;
        }

        :focus-visible {
            outline: var(--focus-ring-width) solid var(--focus-ring);
            outline-offset: var(--focus-ring-offset);
        }
        :focus:not(:focus-visible) { outline: none; }

        ::-webkit-scrollbar { width: 10px; height: 10px; }
        ::-webkit-scrollbar-thumb { background: var(--stone-edge); border-radius: var(--radius-sm); }
        ::-webkit-scrollbar-thumb:hover { background: var(--rune-soft); }
        ::-webkit-scrollbar-track { background: transparent; }
        * { scrollbar-width: thin; scrollbar-color: var(--stone-edge) transparent; }
    }
`;

export const sharedStyles: CSSResult[] = [reset];
