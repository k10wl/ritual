# 012 — Drop decoder v1, unbrand v2 → `decoder`

**Date:** 2026-05-21
**Status:** Approved

## Background

`decoder-text.ts` (v1) and `decoder-v2/` coexist. Every live caller (`ritual-dial`, `dial-telemetry`, `run-addresses`) already wires v2; v1 is referenced only by its own story (`decoder-text.stories.ts`). Design Log 008 left v1 in place "until callers migrate" — migration is done.

Versioned names (`decoder-v2`, `DecoderV2`) leaked the migration into stable code. With v1 gone, the version suffix is dead weight and a future-tense lie: there is no v3 planned, and naming a stable primitive after its migration moment is friction every reader pays.

## Problem

1. Two decoders in the tree; only one is real.
2. Story file imports the dead one, keeping it linked in the bundle.
3. Element tag and class carry a `-v2` / `V2` suffix with no remaining contrast partner.

## Design

### Delete

- `frontend/src/ui/decoder-text.ts`
- `frontend/src/ui/decoder-text.stories.ts`

No re-exports, no compat shim — see [[project_no_backwards_compat]].

### Rename (`git mv` for history — see [[feedback_git_mv]])

```
frontend/src/ui/decoder-v2/         → frontend/src/ui/decoder/
frontend/src/ui/decoder-v2.stories.ts → frontend/src/ui/decoder.stories.ts
```

### Identifier rewrite

| Before          | After       |
|-----------------|-------------|
| `decoder-v2`    | `decoder`   |
| `DecoderV2`     | `Decoder`   |
| `<decoder-v2>`  | `<decoder>` |
| `import "./decoder-v2"` | `import "./decoder"` |

Storybook story class `DecoderV2Cycle` → `DecoderCycle`; tag `decoder-v2-cycle` → `decoder-cycle`; title `"Text / DecoderV2"` → `"Text / Decoder"`.

### Callers updated

- `frontend/src/ui/ritual-dial.ts` — import + 2 tag sites
- `frontend/src/ui/dial-telemetry.ts` — import + 1 tag site
- `frontend/src/ui/run-addresses.ts` — import + 2 tag sites

### Naming guardrail

`decoder` is the contract: scrambles text via ripple. No origin slot, no version. Matches [[feedback_primitives_origin_agnostic]] applied to time-axis ("v2" is an origin in the version dimension).

## Trade-offs

- **Risk of `<decoder>` collision with future native element**: zero — no HTML spec proposal claims the name; custom elements MUST contain a hyphen, native elements MUST NOT, so they cannot collide. (Lit / custom-element naming requires a hyphen — `decoder` *alone* would fail. The tag therefore stays `decoder` with the hyphen rule satisfied via the directory layout? **Correction**: custom element names require a hyphen. Tag must be `decoder-text` or similar. Picking `decoder-text` resurrects the v1 name and confuses git history. Picking `decoder` is invalid.)

  Resolution: tag is `rune-decoder` (project vocabulary — runes per [[project_brand_language]]); class is `RuneDecoder`; directory stays `decoder/` since dir name has no tag constraint.

  | Before          | After            |
  |-----------------|------------------|
  | `decoder-v2`    | `rune-decoder`   |
  | `DecoderV2`     | `RuneDecoder`    |

- **Single-rename churn vs. staged deprecation**: no external consumers; one PR is cheaper than a deprecation window.

## Implementation Plan

1. `git rm frontend/src/ui/decoder-text.ts frontend/src/ui/decoder-text.stories.ts`.
2. `git mv frontend/src/ui/decoder-v2 frontend/src/ui/decoder`.
3. `git mv frontend/src/ui/decoder-v2.stories.ts frontend/src/ui/decoder.stories.ts`.
4. Rewrite identifiers across `frontend/src/ui/decoder/index.ts`, `decoder.stories.ts`, `ritual-dial.ts`, `dial-telemetry.ts`, `run-addresses.ts`:
   - `decoder-v2` → `rune-decoder`
   - `DecoderV2` → `RuneDecoder`
   - `./decoder-v2` → `./decoder`
   - story tag `decoder-v2-cycle` → `rune-decoder-cycle`
   - story class `DecoderV2Cycle` → `RuneDecoderCycle`
   - story title `"Text / DecoderV2"` → `"Text / Rune Decoder"`
5. `pnpm typecheck && pnpm build`.
6. Storybook smoke: open `Text / Rune Decoder` cycle story.
7. Verify dial idle splash, telemetry ETA reveal, RUN addresses decode (per [[feedback_playwright_plugin_not_bridge]]).

## Verification

- `grep -r 'decoder-v2\|DecoderV2\|decoder-text\|DecoderText' frontend/src` returns nothing.
- Storybook renders cycle story without console errors.
- Dial RUN stage shows speed/size decode (telemetry path) and address copy decode (addresses path).
- Type checker passes.

## Open Questions

- ~~**Tag prefix `rune-`**~~ — confirmed 2026-05-21. Fits [[project_brand_language]].
