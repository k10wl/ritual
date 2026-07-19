# Frontend Conventions

## Design Language
- Follow **Apple Human Interface Guidelines**. Defer to HIG over personal taste or generic web patterns.
  - HIG entry: https://developer.apple.com/design/human-interface-guidelines/
  - Foundations: https://developer.apple.com/design/human-interface-guidelines/foundations
  - Patterns: https://developer.apple.com/design/human-interface-guidelines/patterns
  - Components: https://developer.apple.com/design/human-interface-guidelines/components
- Prefer **simplicity**: fewer elements, fewer states, fewer abstractions. Cut anything not earning its space.

## Stack
- **Lit** for components: https://lit.dev/docs/
  - Follow Lit best practices: https://lit.dev/docs/composition/component-composition/
  - Reactive properties over manual DOM, `@state` for internal state, `@property` for public API.
  - Shadow DOM + scoped styles; expose customization via CSS custom properties and `::part`.
  - Controllers (`ReactiveController`) for cross-cutting concerns; avoid mixins and base-class inheritance.
  - Tasks (`@lit/task`) for async data; no ad-hoc loading flags.
- **@lit/context** for cross-tree state: https://lit.dev/docs/data/context/
  - Pass services and shared state via context, not prop-drilling or globals.
  - Define a context key per concern; provide at composition root, consume in leaves.

## Modern Platform First
- **Use modern CSS and JS APIs** — the platform now solves most of what libraries used to. Reach for a dependency only when the platform genuinely can't.
- CSS: container queries, `:has()`, subgrid, nesting, `@layer`, custom properties, `color-mix()`, view transitions, anchor positioning, logical properties, `aspect-ratio`, `clamp()`, scroll-driven animations.
- JS: `structuredClone`, `AbortController`/`AbortSignal`, `Intl.*`, `URL`/`URLSearchParams`, async iterators, top-level await, private class fields, `Object.groupBy`, `Array.prototype.{at,with,toSorted}`.
- DOM: native `<dialog>`, popover API, form-associated custom elements, `ResizeObserver`, `IntersectionObserver`, `MutationObserver`.
- No polyfills, no shims, no utility libs for what a one-liner of modern CSS/JS handles.

## Design System First
- **Start every UI task at the design system**, not the screen. Ask: which primitive/component/token already covers this? If none, design the primitive first, then the screen.
- **Layered composition**:
  1. **Tokens** — CSS custom properties for color, spacing, radius, typography, motion. One source of truth. No magic numbers in components.
  2. **Primitives** — atomic elements (button, input, icon, surface). No business logic.
  3. **Components** — composed from primitives with bound behaviour.
  4. **Screens** — composed from components.
- **Reuse over reinvention**: same affordance → same primitive → same behaviour everywhere. If two surfaces look or behave alike, they share a component, not a copy.

## Storybook is a First-Class Citizen
- Every primitive and component ships with a story. Stories are not optional, not deferred, not "nice to have".
- Build and verify in Storybook before wiring into the app. Stories are the canonical playground and visual spec.
- Treat broken stories as broken code: fix immediately, never let them rot.
- When changing a component, update its story in the same change.

## Adding a Primitive
1. **Audit gate**: ≥2 callers exist or are imminent. One-caller atoms stay inline.
2. **Name**: `rune-<noun>`. Lowercase, single noun, no variant baked into the name (`rune-button`, never `rune-primary-button`).
3. **File layout** under `src/ui/primitives/`:
   - `rune-<name>.ts` — single element, `extends LitElement`.
   - `rune-<name>.stories.ts` — one story per attribute variant + one composition story.
   - `rune-<name>.test.ts` — behavior tests (`@web/test-runner` + `@open-wc/testing`).
4. **Element body**:
   - Variants via attributes (`variant=`, `size=`, `tone=`), styled by `:host([variant="…"]) { … }`.
   - Override surface: component-scoped CSS custom properties (`--rune-<name>-bg`, `--rune-<name>-padding`) + `::part()` where surgical access is needed.
   - `static styles = [...sharedStyles, css\` … \`]` — never declare reset/base in-class.
   - Doc-comment links the relevant HIG page.
5. **Replace inline usage** in the same PR; delete the duplicated CSS/markup.
6. **Re-export** from `src/ui/primitives/index.ts`.
7. **Verify**: Storybook renders + Wails dev build passes (see `skill: verify`).

## Composition Rules
- **Import direction is one-way**: tokens → primitives → components → screens. Never upward.
- **Primitives may** import other primitives + `@lit/context` for environment (theme, motion-prefs).
- **Primitives may not** import components, screens, or `wails-api`. They stay pure-presentation. Data arrives via attributes/properties/slots; behavior exits via custom events.
- **Components may** import primitives + contexts + `wails-api`. Wiring lives here.
- **Slot vocabulary**: default slot = primary content; named slots = `leading`, `trailing`, `hint`, `meta`. Matches HIG list-row vocabulary.
- **Custom event names**: `press` (not `click`), `change`, `open` / `close`, `dismiss`. Native event names are reserved for re-emission.
- **No JS-driven animation in primitives**: CSS `@property` + keyframes only. GSAP and similar are forbidden inside `primitives/`.
- **One element per file**; one primitive per PR. Audit clarity beats batching.
- **Material Web–style element split** is allowed only when a variant is *structurally* different (e.g. `<rune-sheet>` vs an inline popover), never for visual variants of the same affordance.

## Testing Posture
- **Storybook stories** are the visual spec. Every primitive + variant has a story.
- **`@web/test-runner` + `@open-wc/testing`** run headless behavior tests per primitive. Each `rune-<name>.test.ts` covers: attribute → render, attribute → applied styles, event dispatch, slot fill, focus/keyboard behavior.
- **Verify** before merging via `skill: verify` (Storybook + Wails dev build).
- No visual regression / screenshot diffing until a real drift incident justifies the infra.
