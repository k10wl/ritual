# 023 — Disable main-window resize

**Date:** 2026-05-25
**Status:** Draft
**Related:** [[007-hig-ux-coherence]] (single-dial layout assumes a fixed canvas), [[011-dial-frame-flip]] (`.frame { min-height: 480px }` is sized for one window shape), [[020-lit-render-purity]] (entrance animations assume a known viewport).

## Background

`cmd/gui/main.go` creates the main window with `Width: 560, Height: 720, MinWidth: 420, MinHeight: 560` and no maximum, so the OS chrome lets users drag corners and maximise. The logs console (`logsWindow`) is a separate, deliberately resizable window — operators scan long log dumps there.

The dial GUI (007 → 013) was laid out for one canvas. `<ritual-dial>` centres in a `.frame` whose `min-height: 480px` was tuned to "cover the worst-case stage" (011). The under-slot telemetry, address list (010), and Advanced disclosure (014) all assume that canvas.

## Problem

A resizable main window buys nothing and breaks several things:

1. **Layout drift.** Drag the corner small → dial loses centring, addresses row wraps, decoder text overflows. Drag big → empty whitespace dominates and the dial looks lost in a void. Neither is a tested state.
2. **Entrance animations.** 020 already had to special-case "don't run entrance on the empty first frame." A resize event arrives mid-flight and re-triggers nothing — but the *target* geometry changes, so GSAP tweens land at the wrong size.
3. **OS chrome inconsistency vs Mac.** Mac already runs `MacTitleBarHiddenInset` + `InvisibleTitleBarHeight: 50` (cmd/gui/main.go:97-101). Windows shows the full native frame including the maximise button. Resize is already half-suppressed on Mac in spirit (no titlebar to grab), inviting confusion when the same build behaves differently across platforms.
4. **Future custom titlebar.** [[024-custom-titlebar]] (if we adopt it) wants a fixed canvas — a custom drag region + close button is straightforward, a custom resize-grab handle is not.

Resizability is a default we never asked for. Lock it.

## Questions and Answers

**Q1.** Does the logs window also get locked?
**A.** No. Logs are long, variable-width log lines; resize is the right behaviour there. Scope is `mainWindow` only.

**Q2.** Fixed dimensions — what numbers?
**A.** Current `Width: 560, Height: 720`. No reason to change while we're at it; that's the canvas every stage has been tuned against.

**Q3.** Wails v3 field?
**A.** `application.WebviewWindowOptions.DisableResize bool` (verified in v3 alpha — see `go doc github.com/wailsapp/wails/v3/pkg/application.WebviewWindowOptions`). Cross-platform.

**Q4.** What about `MinWidth` / `MinHeight`?
**A.** Drop them. With `DisableResize: true` they are dead fields; keeping them in the source invites the next reader to assume the window is still resizable.

**Q5.** What about the OS maximise button?
**A.** On Windows, `DisableResize` greys out the maximise button but doesn't hide it — the chrome still shows three controls. Set `MaximiseButtonState: application.ButtonHidden` so the chrome shows only minimise + close. (Mac maximise = green traffic-light fullscreen; `MaximiseButtonState` is cross-platform in v3.)

**Q6.** Mac green button fullscreen behaviour?
**A.** With `MaximiseButtonState: ButtonHidden`, the green traffic light is hidden on Mac too. Fullscreen via menu still works via `Ctrl+Cmd+F` unless we explicitly disable it. Acceptable — power users can opt in via menu; default doesn't expose a "make it big" control that breaks the layout.

**Q7.** Anything to change in the frontend?
**A.** No. The point of locking the canvas is that the frontend already assumes it. We could later *remove* defensive media queries / min-height guards, but that's a follow-up cleanup, not part of this change.

**Q8.** Headless smoke / Storybook impact?
**A.** Storybook (005) runs the components in a browser tab at whatever size, not in a Wails window. No change there.

**Q9.** What if someone is on a 1366×768 laptop where 720 is tight?
**A.** Window centres on the screen WorkArea; on 768 that leaves ~24px above + below the chrome — fits. We don't currently support sub-720 displays for any stage layout, so adding a "responsive" fallback would be designing for a hypothetical. If a real user complains, address then.

**Q10.** Does this conflict with [[024-custom-titlebar]]?
**A.** No — it's a prerequisite. A fixed canvas makes the custom titlebar trivially correct (one DOM element, no resize-grab handle needed). If 024 is rejected, 023 still stands on its own.

## Design

Single change site: `cmd/gui/main.go:91-103`, the `mainWindow := wailsApp.Window.NewWithOptions(...)` call.

Before (current):

```go
mainWindow := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
    Name:  "main",
    Title: config.ProductName,
    Width: 560, Height: 720,
    MinWidth: 420, MinHeight: 560,
    BackgroundColour: application.NewRGB(27, 38, 54),
    Mac: application.MacWindow{
        InvisibleTitleBarHeight: 50,
        Backdrop:                application.MacBackdropTranslucent,
        TitleBar:                application.MacTitleBarHiddenInset,
    },
    URL: "/",
})
```

After:

```go
mainWindow := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
    Name:                "main",
    Title:               config.ProductName,
    Width:               560,
    Height:              720,
    DisableResize:       true,
    MaximiseButtonState: application.ButtonHidden,
    BackgroundColour:    application.NewRGB(27, 38, 54),
    Mac: application.MacWindow{
        InvisibleTitleBarHeight: 50,
        Backdrop:                application.MacBackdropTranslucent,
        TitleBar:                application.MacTitleBarHiddenInset,
    },
    URL: "/",
})
```

Diff in plain words:

- ✅ `DisableResize: true` — locks the corner-drag.
- ✅ `MaximiseButtonState: application.ButtonHidden` — removes the (now dead) maximise chrome.
- ❌ `MinWidth: 420, MinHeight: 560` removed — dead fields under DisableResize.
- Logs window untouched.

## Implementation Plan

**Phase A — apply the four-line change.**

1. Edit `cmd/gui/main.go` per Design block above.
2. `go build ./...` — confirm `application.ButtonHidden` is the right constant name; if v3 alpha calls it `ButtonStateHidden` or similar, use the actual symbol. (Verify with `go doc github.com/wailsapp/wails/v3/pkg/application.ButtonState`.)

**Phase B — smoke.**

1. `go run ./cmd/gui` on Windows: confirm window is 560×720, corner-drag does nothing, no maximise button in the chrome, minimise + close still work.
2. Same on Mac if available: confirm green traffic-light is gone, red + yellow still present, double-click on the (invisible) titlebar no longer maximises.
3. Logs window still resizable end-to-end.

**Phase C — none.** No follow-up sweep. Defensive `min-width:` rules in the frontend can stay; they cost nothing and document intent.

## Verification

- Window dimensions stay at exactly 560×720 across the entire session, every stage of [[007-hig-ux-coherence]] (IDLE / PREP / RUN / FINAL / LOCKED / FAILED).
- No way for an operator to reach a non-560×720 main window state via mouse interaction.
- Logs console behaviour is unchanged (resizes freely, retains 960×640 default).

## Trade-offs

- **Lost.** Power-users on 4K monitors can't enlarge the dial. Counter: the dial's pixel layout is tuned, not fluid — enlarging would just upscale; macOS/Windows accessibility zoom handles the real "I need it bigger" case.
- **Gained.** One canvas = one set of layout assumptions; entrance animations (020) operate on known geometry; [[024-custom-titlebar]] becomes viable without a resize-handle hack.
