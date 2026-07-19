# 024 — Custom unified titlebar (exploratory)

**Date:** 2026-05-25
**Status:** Draft — exploratory; defer decision until after [[023-disable-resize]] ships
**Related:** [[023-disable-resize]] (prerequisite — fixed canvas), [[007-hig-ux-coherence]] (single-dial visual identity), [[015-design-system]] (`rune-*` primitive layer for any new chrome element).

## Background

`cmd/gui/main.go` creates the main window with Mac-specific chrome already half-customised:

```go
Mac: application.MacWindow{
    InvisibleTitleBarHeight: 50,
    Backdrop:                application.MacBackdropTranslucent,
    TitleBar:                application.MacTitleBarHiddenInset,
}
```

On Mac, the native traffic lights float over an invisible 50px titlebar and the app reads as one piece. On Windows the same build shows the full native frame — title text, icon, minimise/maximise/close in the standard chrome strip — and the dial UI looks like a widget *inside* an OS window rather than the app itself.

[[023-disable-resize]] removes the maximise button and locks the canvas, but the rest of the Windows chrome remains: title bar background, icon, OS-controlled colors.

## Problem

The visual identity established by 007 (single morphing dial, Steam-inspired) is fractured on Windows by an OS-themed strip the design system can't reach. Three workflows make it worse:

1. **Branding.** Departure Mono / rune-* tokens (015) define the brand inside the webview; the Win32 titlebar uses the system theme. Light-mode Windows ⇒ white strip above a dark dial.
2. **Drag region affordance.** Mac's hidden-inset gives a 50px drag region near the top. Windows has no equivalent unless the OS chrome stays. Removing OS chrome means we must provide our own drag region.
3. **Future surfaces.** If we ever want logs-window toggle, gear (014), or sync-upstream (021) accessible *outside* the dial body, the titlebar is the natural home — but only if we control it.

User signal (this run, 2026-05-25): **"not sure if we'll actually use it."** Treat as exploratory; resolve the trade-offs before committing implementation effort.

## Questions and Answers

**Q1.** Scope — Windows-only, or unified both platforms?
**A.** User asked for unified (both). Means hiding Mac's traffic lights too and re-creating close/minimise. Risk: traffic-light re-creation is fiddly (color, hover behaviour, accessibility expectations baked into macOS muscle memory). Counter-question: does "unified" actually mean "looks-the-same-everywhere" or "feels-native-everywhere"? Latter would keep traffic lights on Mac and only customise Windows. *Pending user clarification — see Q10.*

**Q2.** Wails v3 field?
**A.** `WebviewWindowOptions.Frameless bool` (verified). Cross-platform. Removes the entire OS chrome including drag region — frontend must provide its own.

**Q3.** How does the webview get a drag region without OS chrome?
**A.** CSS: `-webkit-app-region: drag` on the titlebar element, `-webkit-app-region: no-drag` on any interactive child (buttons). Standard Wails pattern, supported in v3.

**Q4.** Close / minimise buttons — call into Wails?
**A.** Yes — `window.close()`, `window.minimise()` via the `@wailsio/runtime` JS binding. (Maximise omitted; 023 already hides it.) Need to confirm the exact JS API surface in v3 alpha before committing.

**Q5.** What does the titlebar contain?
**A.** **User answered: not decided.** Open list to evaluate:
  - App title ("Ritual") — pure brand, zero function.
  - Close + minimise buttons — required if `Frameless: true`.
  - Logs window toggle — promotes a hidden affordance.
  - Gear / settings — moves the 014 disclosure target out of the dial body.

  v1 minimum: drag region + close + minimise. Everything else is opt-in once we know the bar is staying.

**Q6.** Height?
**A.** Match Mac's existing 50px so the dial canvas position is consistent across platforms.

**Q7.** Mac drag region with Frameless?
**A.** If we go fully Frameless on Mac, we lose `InvisibleTitleBarHeight` and `MacTitleBarHiddenInset` semantics. The DOM drag region works on Mac too. Risk: traffic lights default position is owned by `MacTitleBarHiddenInset`; without it we'd be drawing fake ones. See Q1 — this is the crux of "unified."

**Q8.** Accessibility / keyboard?
**A.** `Alt+F4` (Win) / `Cmd+Q` (Mac) still close the app via the Wails event loop — Frameless does not break the OS event hooks. Close button is mouse convenience, not the only path.

**Q9.** What if Frameless creates a worse experience than we have?
**A.** Acceptable to land 023 and stop. 023 stands alone. This log can sit Draft and be revisited if we find a concrete need.

**Q10.** *Open for user.* Does "unified" mean:
  - (a) **Identical chrome both platforms** — hide traffic lights, draw our own close/min on both. High fidelity, breaks Mac muscle memory.
  - (b) **Identical brand, native controls** — keep `MacTitleBarHiddenInset` (traffic lights stay), add a custom bar only on Windows that visually matches the dial. Lower risk, asymmetric code.
  - (c) **Skip entirely** — 023 alone is enough; OS chrome differences are tolerable.

  Recommend (b) — it is what Slack, Discord, VSCode, Figma all do (custom Windows titlebar, native Mac traffic lights). The "Mac feels native, Windows feels branded" split is the industry norm and matches the existing half-customisation in `main.go`.

## Design Sketches

Sketch only — no commitment. Three variants, each behind one prerequisite (023 shipped).

### Variant A — Windows-only custom bar (Q10b, recommended)

```go
mainWindow := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
    Name:                "main",
    Title:               config.ProductName,
    Width:               560,
    Height:              720,
    DisableResize:       true,                       // 023
    MaximiseButtonState: application.ButtonHidden,   // 023
    Frameless:           runtime.GOOS == "windows",  // NEW
    BackgroundColour:    application.NewRGB(27, 38, 54),
    Mac: application.MacWindow{
        InvisibleTitleBarHeight: 50,
        Backdrop:                application.MacBackdropTranslucent,
        TitleBar:                application.MacTitleBarHiddenInset,
    },
    URL: "/",
})
```

Frontend adds `<rune-titlebar>` (per 015 primitive layer) that renders only when the webview detects Frameless mode. Detection: a `data-frameless="true"` attribute set by Wails JS init, or feature-detect via `window.runtime.IsFrameless()` if v3 exposes one.

### Variant B — Unified both platforms (Q10a)

Same Go config but `Frameless: true` unconditionally; Mac block drops `TitleBar` and `InvisibleTitleBarHeight`. Frontend always renders `<rune-titlebar>`, including faux traffic lights on Mac. Higher risk.

### Variant C — Don't ship

Close this log as `Superseded by 023` once 023 lands and the Windows chrome is judged tolerable.

## Implementation Plan (conditional on Variant A approval)

**Phase A — Go side.**

1. Add a small platform helper (`runtime.GOOS == "windows"`) directly in `main.go` or factor into `internal/config` if used elsewhere.
2. Set `Frameless: true` conditionally; leave Mac block intact.

**Phase B — Frontend primitive.**

1. New `frontend/src/components/rune-titlebar.ts` (015 conventions): drag region full width, close + minimise buttons on the right (Windows convention) with `-webkit-app-region: no-drag`, app title left-aligned, 50px height, brand tokens.
2. Wire close/minimise to `window.close()` / `window.minimise()` via `@wailsio/runtime`.
3. Conditional render in `ritual-app.ts` — hidden on non-Frameless windows (Mac default).

**Phase C — Storybook.**

1. Story for `<rune-titlebar>` standalone; mock window methods via the existing Storybook transport-intercept (005).

**Phase D — smoke.**

1. Windows: confirm dragging the titlebar moves the window, close/min work, no native chrome.
2. Mac: confirm titlebar element absent, traffic lights present, drag region still works.

## Verification

- Windows main window has no OS chrome; in-DOM titlebar handles drag + close + minimise.
- Mac main window unchanged (traffic lights still float over invisible 50px region).
- App title brand-consistent on Windows ("Ritual" in Departure Mono on a `--bg-elev-1` strip).
- No regression in `Alt+F4` / `Cmd+Q` shutdown paths (lifecycle drain still runs per `WindowClosing` hook in `main.go:109`).

## Trade-offs

- **Asymmetric code** (Variant A) — two paths to maintain, but each is short and the platform branch is explicit.
- **Lost native Win11 features** — snap layouts (Win+Z), Aero shake, OS context menu on titlebar. Mitigation: 023 already hides maximise; snap would resize to non-560×720 and is wrong for this app.
- **Gained surface for future affordances** — gear, logs toggle, sync-upstream all have a natural docking site.

## Decision needed before implementation

User: Pick Q10 (a / b / c). Recommendation: **b**. If c, mark this log `Superseded by 023` and move on.
