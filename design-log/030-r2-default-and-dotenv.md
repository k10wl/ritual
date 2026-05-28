# 030 — R2 as default backend + runtime `.env` loader

**Date:** 2026-05-25
**Status:** Implemented (with Taskfile-aligned deviation, see §Implementation Results)
**Related:** [[022-divergence-from-origin-delta-sync]] (back-port candidate `76c0682` from origin — Task-only dotenv, never landed locally), [[016-live-sync-resurrected]] (production storage path).

## Background

`cmd/gui/main.go:222` selects the remote backend via a compile-time constant:

```go
const remoteMode = remote.ModeMock  // or ModeR2
```

To use R2 today an operator must:

1. **Edit the constant** to `remote.ModeR2`.
2. **Rebuild** the binary.
3. **Set four env vars** in the launching shell before each session:
   - `RITUAL_R2_BUCKET`, `RITUAL_R2_ACCOUNT_ID`, `RITUAL_R2_ACCESS_KEY_ID`, `RITUAL_R2_SECRET_ACCESS_KEY`.

If any var is missing, `remote.buildR2FromEnv` (build.go:93-121) returns `ErrR2EnvIncomplete` and the GUI fails to boot.

Origin's commit `76c0682` partially addressed this with a **Taskfile-only** `dotenv:` directive that auto-loads `.env.dev.local`. That helps `task gui:dev` users but does nothing for `go run ./cmd/gui` or shipped binaries.

## Problem

Two coupled gaps:

1. **R2 is not "in the binary."** Operators must hand-edit + rebuild to flip backends. A shipped binary is locked to whichever mode was compiled in.
2. **No runtime `.env` reader.** Credentials must be set in the shell each session, or `setx`-d into the user environment globally. There's no project-local way to hold creds securely.

A reasonable production posture:

- One binary that knows how to talk to R2.
- `.env.local` (gitignored) holds credentials next to the binary or repo root.
- Mock backend remains available — useful for testing, demos, and air-gapped dev — but is **opt-in**, not the default.

## Questions and Answers

**Q1.** Default backend — R2, mock, or auto-detect?
**A.** R2. Mock is a dev affordance; new users with creds should get R2 without code edits. Auto-detect ("R2 if creds present, else mock") feels clever but masks misconfiguration — an operator who *thinks* they're hitting R2 because they typed in creds, but typoed a key name, would silently land in mock and not notice until they look for their data in the wrong place. Fail-fast is safer.

**Q2.** How does mock stay reachable?
**A.** Runtime env var: `RITUAL_REMOTE_MODE=mock` switches to mock. Default (unset / any other value) is R2. Documented in the Mode comment in `remote/build.go`. No rebuild to flip.

**Q3.** Drop the `remoteMode` constant from `main.go` entirely?
**A.** Yes. Replace with a single call:

```go
rawRemote, err := remote.Build(ctx, remote.ResolveModeFromEnv(), bus)
```

`ResolveModeFromEnv()` reads `RITUAL_REMOTE_MODE` and returns the corresponding `Mode`. Default branch returns `ModeR2`. Keeps the type-safe `Mode` enum but lifts selection out of compile time.

**Q4.** `.env` loader — write our own or use a library?
**A.** Library. `github.com/joho/godotenv` is the de-facto standard, ~400 LoC, zero transitive deps, well-tested. Writing our own is 50-70 lines for the parser and another 30 for tests; net code-debt loss is small but maintenance is ours forever. **Recommendation: godotenv.** If the dependency-hygiene answer is "no," fall back to a small inline parser — the .env format is trivial (key=value, ignore # and blank lines, optional quotes).

**Q5.** Which `.env` files do we look for, and in what order?
**A.** Looking-glass order, first match wins per key (godotenv-standard):
  1. `.env.local` — operator-private, gitignored.
  2. `.env` — repo-default, may be checked in with placeholder values for documentation.

  We don't bother with `.env.{development,production}.local` chains — single-binary project, no NODE_ENV-style modes. Two files is enough.

**Q6.** When does the loader run?
**A.** Very early in `main()`, **before** `buildRuntime()`. Failures are non-fatal (a missing `.env.local` is a normal state for fresh checkouts); only credentials *resolution* fails-fast inside `remote.buildR2FromEnv`. Loader behavior: try to open each file in order; if it exists and is unreadable, log a warning and continue.

**Q7.** Working directory for the `.env` lookup?
**A.** Two-pass:
  1. Next to the executable (`os.Executable()` + `..`) — works for shipped binaries.
  2. Repository root / CWD — works for `go run ./cmd/gui` and dev workflow.
  Use the executable-adjacent file if present; else CWD. Documented in the loader comment.

**Q8.** `.gitignore` entries?
**A.** Add `.env`, `.env.local`, `.env.*.local`. The middle one is the operator-private credentials file; the outer two cover common variants. We do *not* ignore example placeholders like `.env.example` — those can be checked in.

**Q9.** Should the loader also overwrite already-set env vars?
**A.** No. Operator shell exports beat `.env.local` entries. godotenv's `Load()` matches this — it never overwrites. Useful for CI: `.env.local` doesn't exist, env vars come from secrets store.

**Q10.** Logging — does the loader announce success/failure?
**A.** One-line `log.Printf("dotenv: loaded N keys from %s", path)` per file actually loaded; nothing if the file's absent. No credential values in logs — only filenames and key counts.

**Q11.** Mock + R2 env-var collision?
**A.** None. `RITUAL_REMOTE_MODE` and `RITUAL_R2_*` are independent. Setting both is sensible: "use mock today, but my R2 creds are ready when I flip the toggle."

**Q12.** Tests — how do we cover the loader without polluting the test process's env?
**A.** Library tests use `os.Setenv` + `t.Cleanup(func(){ os.Unsetenv(...) })` per test; godotenv's API supports loading from arbitrary paths so we don't need a magic CWD-aware test helper. Add a `loader_test.go` next to the loader covering: missing file (no-op), existing file (vars set), overwrite-precedence (existing env wins), bad-format (parser error surfaced).

**Q13.** Back-port the Taskfile `dotenv:` directive from `76c0682`?
**A.** Yes — additive, no conflict. The Taskfile gains an `env:` block remapping `R2_BUCKET_NAME` → `RITUAL_R2_BUCKET` etc. **But** with this log shipping a Go-side loader, the Taskfile directive becomes redundant for any launch that goes through `main()`. Keep it anyway: Task-spawned subprocesses other than the GUI itself (e.g. integration tests) benefit, and it's a single block.

**Q14.** Move the comment block in `main.go` — the existing 20-line "HOW TO FLIP MOCK → REAL R2" is now mostly stale.
**A.** Yes. Replace with a 4-line summary pointing at the new env var + `.env.local`. The detailed how-to belongs in `docs/build.md` (or a new `docs/r2-setup.md`).

## Design

### Add the loader

```go
// internal/config/dotenv.go (new file)

// Package-internal because callers should never touch parsing directly —
// LoadEnvFiles is the only entry point and it's called once from main().

package config

import (
    "log"
    "os"
    "path/filepath"

    "github.com/joho/godotenv"
)

// LoadEnvFiles reads .env.local then .env from two locations:
//   1. The directory containing the running executable.
//   2. The current working directory.
// First-existing wins per location. Already-set process env vars are never
// overwritten (godotenv.Load semantics). Failures are logged and non-fatal.
func LoadEnvFiles() {
    candidates := envCandidatePaths()
    for _, p := range candidates {
        if _, err := os.Stat(p); err != nil {
            continue
        }
        if err := godotenv.Load(p); err != nil {
            log.Printf("dotenv: load %s: %v (continuing)", p, err)
            continue
        }
        log.Printf("dotenv: loaded %s", p)
    }
}

func envCandidatePaths() []string {
    var out []string
    if exe, err := os.Executable(); err == nil {
        dir := filepath.Dir(exe)
        out = append(out, filepath.Join(dir, ".env.local"), filepath.Join(dir, ".env"))
    }
    cwd, err := os.Getwd()
    if err == nil {
        out = append(out, filepath.Join(cwd, ".env.local"), filepath.Join(cwd, ".env"))
    }
    return out
}
```

### Add the mode resolver

```go
// internal/subsystems/remote/build.go — append:

// EnvRemoteMode toggles the remote backend at runtime. Values: "mock"
// → ModeMock; anything else (including unset) → ModeR2. Documented as
// the way to opt into the dev-only local-FS mock without rebuilding.
const EnvRemoteMode = "RITUAL_REMOTE_MODE"

// ResolveModeFromEnv returns the Mode requested by RITUAL_REMOTE_MODE.
// Default: ModeR2 — production posture per design-log/030.
func ResolveModeFromEnv() Mode {
    if os.Getenv(EnvRemoteMode) == "mock" {
        return ModeMock
    }
    return ModeR2
}
```

### Wire both into `main.go`

```go
func main() {
    config.LoadEnvFiles()  // NEW: very first thing in main()
    runtime, err := buildRuntime()
    // ... unchanged
}

func buildRuntime() (*guiRuntime, error) {
    // ... unchanged through line 222 ...

    // Old comment block (20+ lines about flip-and-rebuild) → 4 lines:
    //
    // Remote backend selected by RITUAL_REMOTE_MODE env var. Default
    // ModeR2 reads credentials from RITUAL_R2_* (typically loaded from
    // .env.local at startup). Set RITUAL_REMOTE_MODE=mock to use the
    // local-FS dev backend. See docs/r2-setup.md.
    rawRemote, err := remote.Build(ctx, remote.ResolveModeFromEnv(), bus)
}
```

### `.gitignore`

Append:

```
# Operator-private credentials
.env
.env.local
.env.*.local
```

### `.env.example` (new, checked-in placeholder)

```
# Copy to .env.local and fill in your R2 credentials.
# Never commit .env.local — see .gitignore.

RITUAL_R2_BUCKET=
RITUAL_R2_ACCOUNT_ID=
RITUAL_R2_ACCESS_KEY_ID=
RITUAL_R2_SECRET_ACCESS_KEY=

# Optional: opt into the local-FS mock instead of R2.
# Leave commented to use R2 (the default).
# RITUAL_REMOTE_MODE=mock
```

### Taskfile back-port (from 76c0682)

```yaml
# Taskfile.yml — top-level, additive to the existing structure.
dotenv: ['.env.local', '.env']
env:
  RITUAL_R2_BUCKET: '{{.R2_BUCKET_NAME}}'
  RITUAL_R2_ACCOUNT_ID: '{{.R2_ACCOUNT_ID}}'
  RITUAL_R2_ACCESS_KEY_ID: '{{.R2_ACCESS_KEY_ID}}'
  RITUAL_R2_SECRET_ACCESS_KEY: '{{.R2_SECRET_ACCESS_KEY}}'
```

Origin used `R2_BUCKET_NAME` as the Taskfile-side key (vs the runtime's `RITUAL_R2_*`) because that's the variable name a Cloudflare dashboard exports. The Taskfile remap preserves that convenience. If we don't care about the dashboard name compatibility, simplify to direct keys.

## Implementation Plan

**Phase A — dependency + loader.**

1. `go get github.com/joho/godotenv@latest`.
2. Add `internal/config/dotenv.go` per Design.
3. Add `loader_test.go` covering missing/existing/overwrite/bad-format.

**Phase B — mode resolver.**

1. Append `EnvRemoteMode` const + `ResolveModeFromEnv()` to `remote/build.go`.
2. Update `remote/build_test.go` to cover the resolver: unset → R2, "mock" → mock, "r2" → R2, garbage → R2.

**Phase C — wire into `main.go`.**

1. Call `config.LoadEnvFiles()` at top of `main()`.
2. Replace the `const remoteMode = ...` line + `Build(ctx, remoteMode, bus)` call with `Build(ctx, remote.ResolveModeFromEnv(), bus)`.
3. Replace the stale comment block with the 4-line summary.

**Phase D — gitignore + example.**

1. Append the three patterns to `.gitignore`.
2. Add `.env.example` per Design.

**Phase E — Taskfile back-port.**

1. Add the `dotenv:` and top-level `env:` blocks per origin's `76c0682`.

**Phase F — docs.**

1. New `docs/r2-setup.md` (or section in existing `docs/build.md`) covering: getting R2 creds, the four env vars, `.env.local` workflow, `RITUAL_REMOTE_MODE=mock` opt-out.

**Phase G — smoke.**

1. Fresh checkout: copy `.env.example` → `.env.local`, fill creds, `go run ./cmd/gui`. Logs should show `dotenv: loaded .../.env.local` and `store=r2::<bucket>`.
2. Without `.env.local`: `go run ./cmd/gui` should fail with `ErrR2EnvIncomplete` naming all four missing vars.
3. With `RITUAL_REMOTE_MODE=mock` set: should boot regardless of R2 creds, log `store=fs::remote`.

## Verification

- Single binary on disk works for both R2 (default) and mock (env-toggled), no rebuild.
- `.env.local` next to the executable OR in the working directory is read at startup.
- Shell-exported env vars take precedence over `.env.local` (CI safety).
- Boot fails fast with named missing vars when R2 is selected and credentials are incomplete.
- `git status` after `cp .env.example .env.local && edit .env.local` shows `.env.local` ignored.

## Trade-offs

- **New dependency (godotenv).** Tiny library, but it's still surface to maintain. Acceptable — alternative is owning a parser forever.
- **One more file in the repo (.env.example).** Helps onboarding; cost is nil.
- **Existing `remoteMode` callers / tests.** The constant is referenced only in `main.go`; no other call sites. Tests in `remote/build_test.go` test `Build()` directly with explicit modes — unaffected.
- **Run-as-root / restricted environments.** `os.Executable()` may fail or return a sandboxed path; we fall through to CWD. Acceptable.

## Out of scope (follow-ups)

- **Secrets in OS keychain** (Windows Credential Manager / macOS Keychain). The `.env.local` approach is fine for a desktop tool; keychain integration is a separate quality-of-life log if the team ever wants it.
- **Multiple R2 bucket profiles** (e.g. dev bucket vs prod bucket). Could add `RITUAL_PROFILE=dev` → loads `.env.dev.local`. Not needed yet.
- **CI integration.** Documenting how to plumb GitHub Actions secrets / etc. into the test runner; belongs in `docs/`.

## Implementation Results

Default backend was already flipped to R2 by commit 84bf4de (`const remoteMode = remote.ModeR2`), which also back-ported the Taskfile `dotenv:` block. Remaining gap: the **runtime** Go-side loader + mode resolver. Shipped now.

**Deviation — Taskfile-aligned file naming.** The shipped Taskfile uses `.env.{{.RITUAL_ENV}}.local` (default `dev`) + remaps `R2_BUCKET_NAME` → `RITUAL_R2_BUCKET` etc. (`Taskfile.yml:48-54`). To keep one convention across `task gui:*` flows and the shipped binary, the Go loader was aligned to the Taskfile rather than to §Q5's simpler `.env.local`/`.env` pair. The remap is mirrored Go-side so a single `.env.dev.local` works in both paths.

Changes:

- `go.mod` — added direct dependency on `github.com/joho/godotenv v1.5.1`.
- `internal/config/dotenv.go` (new) — `LoadEnvFiles()` reads `.env.{$RITUAL_ENV}.local` (default `dev`) from exe-adjacent dir then CWD; first match wins. `mirrorTaskfileNames()` mirrors `R2_*` → `RITUAL_R2_*` only when the runtime name isn't already set. Operator shell exports beat both file values and the remap (`godotenv.Load` semantics + per-key guard).
- `internal/config/dotenv_test.go` (new) — 6 cases: default profile, profile selector (`dev`/`prod`), missing file no-op, shell-beats-file, remap, remap-respects-shell.
- `internal/subsystems/remote/build.go` — added `EnvRemoteMode` const + `ResolveModeFromEnv() Mode`. Default `ModeR2`, only `"mock"` selects `ModeMock`. Replaced the stale "flip the constant + rebuild" prose at the top of the package with a one-liner pointing at design-log/030.
- `internal/subsystems/remote/build_test.go` — added `TestResolveModeFromEnv` (4 subcases: unset, "mock", "r2", garbage).
- `cmd/gui/main.go` — `config.LoadEnvFiles()` is the first call in `main()`. The 20-line "HOW TO FLIP MOCK → REAL R2" comment block + `const remoteMode = …` line collapsed into a 5-line summary + `remote.Build(ctx, remote.ResolveModeFromEnv(), bus)`.
- `.gitignore` / `.env.example` — already shipped (commit 84bf4de). No change.
- `docs/r2-setup.md` — deferred; the new shape is self-documenting via the `EnvRemoteMode` / `EnvR2*` constants and the `dotenv: loaded …` log line.

Verification:

- `go build ./...` clean.
- `go test ./...` — 32 packages green, including the new config + remote subcases.
- Smoke against the actual app deferred to next operator session: `RITUAL_REMOTE_MODE=mock` should boot without creds; default boot should log `dotenv: loaded …/.env.dev.local` and `store=r2::<bucket>`.
