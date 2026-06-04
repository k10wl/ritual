import type { Preview } from "@storybook/web-components-vite";
import { html } from "lit";
import { setTransport } from "@wailsio/runtime";
import { JoinAddress, Phase, Stage, SyncStatus, ViewModel, Level, type ServerLog } from "../src/wails-api";

declare global {
    interface Window {
        _wails?: { dispatchWailsEvent(e: { name: string; data: unknown }): void };
    }
}

// Method IDs mirror frontend/bindings/ritual/internal/gui/control/controlservice.ts.
// Regenerate bindings (task gui:bindings) keeps Go ↔ JS in sync; this list moves with them.
const M = {
    GetSnapshot: 2954345432,
    OpenRootFolder: 3559753188,
    Dismiss: 3052956532,
    SendConsole: 344753175,
    ShowLogs: 3352242658,
    Start: 4262819292,
    Stop: 4133576568,
    Download: 686511792,
    Upload: 297268657,
    GetSyncStatus: 3603871625,
};
const OBJ_CALL = 0;
const OBJ_EVENTS = 3;

const fixtures = {
    idle: () => new ViewModel({ stage: Stage.StageIdle, phase: Phase.PhaseIdle }),
    // Lock conflict folded into Failed per design-log/017.
    lockConflict: () =>
        new ViewModel({
            stage: Stage.StageFailed,
            phase: Phase.PhaseFailed,
            lockHolder: "alice",
            errorText: "already locked by alice",
        }),
    downloading: (progress: number) =>
        new ViewModel({
            stage: Stage.StageDownloading,
            phase: Phase.PhaseDownloading,
            progress,
            bytesDone: progress * 10_000_000,
            bytesTotal: 1_000_000_000,
            speedMbps: 32,
            etaSeconds: Math.max(0, Math.round((100 - progress) * 1.2)),
        }),
    preparing: () =>
        new ViewModel({
            stage: Stage.StageDownloading,
            phase: Phase.PhasePreparing,
            bytesDone: 1_000_000_000,
            bytesTotal: 1_000_000_000,
        }),
    playing: () =>
        new ViewModel({
            stage: Stage.StageRunning,
            phase: Phase.PhasePlaying,
            addresses: [
                new JoinAddress({ label: "LAN", address: "192.168.1.10:25565" }),
                new JoinAddress({ label: "Tailscale", address: "100.64.0.5:25565" }),
            ],
        }),
    wrapping: () =>
        new ViewModel({ stage: Stage.StageUploading, phase: Phase.PhaseWrapping }),
    saving: (progress: number) =>
        new ViewModel({
            stage: Stage.StageUploading,
            phase: Phase.PhaseSaving,
            progress,
            bytesDone: progress * 10_000_000,
            bytesTotal: 1_000_000_000,
            speedMbps: 22,
            etaSeconds: Math.max(0, Math.round((100 - progress) * 1.2)),
        }),
};

let current: ViewModel = fixtures.idle();
let timer: ReturnType<typeof setInterval> | null = null;
// Tracks whether the in-flight run was launched skip-sync (design-log/036) so
// Stop can narrate the honest tail: a normal run saves (wrapping → saving →
// idle), a skip-sync run saves nothing (wrapping → idle).
let skipSyncRun = false;

// Controllable sync verdict (design-log/035). The interactive Live story flips
// behind/dirty/unpushed via its boolean controls; M.GetSyncStatus returns this.
// Default = clean so the base Live story is quiet unless a story opts in.
let syncStatus: SyncStatus = new SyncStatus({
    behind: false,
    dirty: false,
    unpushed: false,
    localHead: "",
    remoteHead: "",
});

export const setSyncStatus = (s: Partial<SyncStatus>) => {
    syncStatus = new SyncStatus({
        behind: false,
        dirty: false,
        unpushed: false,
        localHead: "",
        remoteHead: "",
        ...s,
    });
};

// Extract the skip-sync flag from a Start call. Wails' Call.ByID packs the
// method's positional params into `args.args` — Start(port, memory, skipSync),
// so skipSync is args.args[2]. Robust to the array being absent (defaults false).
const startSkipSync = (args: unknown): boolean => {
    const a = (args as { args?: unknown[] } | undefined)?.args;
    return Array.isArray(a) ? Boolean(a[2]) : false;
};

// SendConsole(line) packs its single string param at args.args[0].
const consoleArg = (args: unknown): string => {
    const a = (args as { args?: unknown[] } | undefined)?.args;
    return Array.isArray(a) && typeof a[0] === "string" ? a[0] : "";
};

const set = (vm: ViewModel) => {
    current = vm;
    window._wails?.dispatchWailsEvent({ name: "ritual:view", data: vm });
};

const cancelTimer = () => {
    if (timer !== null) {
        clearInterval(timer);
        timer = null;
    }
};

const ramp = (
    make: (p: number) => ViewModel,
    onDone: () => void,
    stepMs = 90,
    stepPct = 1,
) => {
    cancelTimer();
    let p = 0;
    set(make(0));
    timer = setInterval(() => {
        p += stepPct;
        if (p > 100) {
            cancelTimer();
            onDone();
            return;
        }
        set(make(p));
    }, stepMs);
};

setTransport({
    async call(objectID, _method, _windowName, args) {
        if (objectID === OBJ_EVENTS) return undefined;
        if (objectID !== OBJ_CALL) return undefined;
        const methodID = (args as { methodID?: number } | undefined)?.methodID;
        switch (methodID) {
            case M.GetSnapshot:
                return current;
            case M.Start:
                skipSyncRun = startSkipSync(args);
                if (skipSyncRun) {
                    // Skip-sync local-only launch (design-log/036): for testing —
                    // NOT pulling and NOT pushing, and it saves nothing. No
                    // download ramp; straight to a brief preparing beat, then
                    // playing.
                    cancelTimer();
                    set(fixtures.preparing());
                    setTimeout(() => set(fixtures.playing()), 1200);
                    return undefined;
                }
                // Walk download → preparing → playing per design-log/017.
                ramp(fixtures.downloading, () => {
                    set(fixtures.preparing());
                    setTimeout(() => set(fixtures.playing()), 1200);
                });
                return undefined;
            case M.Stop:
                if (skipSyncRun) {
                    // Skip-sync saves nothing (design-log/036 §Q2): wrapping →
                    // idle, NO saving ramp — there is no ref write to narrate.
                    set(fixtures.wrapping());
                    setTimeout(() => set(fixtures.idle()), 1200);
                    return undefined;
                }
                // Walk wrapping → saving → idle.
                set(fixtures.wrapping());
                setTimeout(() => {
                    ramp(fixtures.saving, () => set(fixtures.idle()));
                }, 1200);
                return undefined;
            case M.SendConsole: {
                // Mirror running.ConsoleEchoInfo (design-log/042 §Q8): echo the
                // command back as a kind:"in" row on the "confirmed write", then
                // a synthetic server response after a beat. Wire-driven, exactly
                // like the backend — the console renders nothing optimistically.
                const text = consoleArg(args);
                if (text.trim() !== "") {
                    emitServerLine({ ts: Date.now(), kind: "in", level: Level.$zero, text });
                    const ack = mockServerAck(text);
                    if (ack) {
                        setTimeout(
                            () => emitServerLine({ ts: Date.now(), kind: "out", level: Level.$zero, text: ack }),
                            140,
                        );
                    }
                }
                return undefined;
            }
            case M.Dismiss:
                set(fixtures.idle());
                return undefined;
            case M.GetSyncStatus:
                // Controllable verdict (design-log/035): the interactive Live
                // story sets this via setSyncStatus() so reviewers can surface the
                // IDLE "Unpublished changes" cue and the Sync pane's Publish action
                // + loud behind-warning without a live backend. Live behaviour is a
                // remote-vs-local HEAD compare merged with a workdir-dirty scan.
                return syncStatus;
            case M.Download:
                // Download is ONE honest beat (design-log/031 addendum): ⬇
                // "Downloading" filling to 100%, then idle. No prepare, no save.
                ramp(fixtures.downloading, () => set(fixtures.idle()));
                return undefined;
            case M.Upload:
                // Upload is ONE honest beat (design-log/031 addendum): ⬆
                // "Saving" filling to 100%, then idle. No spin-up, no spin-down.
                ramp(fixtures.saving, () => set(fixtures.idle()));
                return undefined;
            default:
                return undefined;
        }
    },
});

export const pushView = (vm: unknown) =>
    window._wails?.dispatchWailsEvent({ name: "ritual:view", data: vm });

export const pushServerLogs = (batch: unknown) =>
    window._wails?.dispatchWailsEvent({ name: "server:logs", data: batch });

// --- Storybook console engine ----------------------------------------------
// Mirrors the Go batchingLogEmitter ring (cmd/gui/main.go, design-log/006/042):
// a lazy-timer ring that coalesces emitted lines into `server:logs` batches,
// idle-quiescent (no timer at rest), drop-oldest on overflow with a count.
// Stories feed it one line at a time; it spits out the batches just like the
// backend, so the console exercises the real wire shape — not pre-baked batches.
const CONSOLE_CAP = 1024;
const CONSOLE_BATCH_MAX = 128;
const CONSOLE_INTERVAL = 16;

let consoleRing: ServerLog[] = [];
let consoleDropped = 0;
let consoleTimer: ReturnType<typeof setTimeout> | null = null;

const flushConsole = () => {
    consoleTimer = null;
    if (consoleRing.length === 0 && consoleDropped === 0) return;
    const n = Math.min(consoleRing.length, CONSOLE_BATCH_MAX);
    const lines = consoleRing.slice(0, n);
    consoleRing = consoleRing.slice(n);
    const dropped = consoleDropped;
    consoleDropped = 0;
    pushServerLogs({ lines, dropped });
    // BATCH_MAX capped this flush — re-arm so leftover doesn't wait idle.
    if (consoleRing.length > 0 && consoleTimer === null) {
        consoleTimer = setTimeout(flushConsole, CONSOLE_INTERVAL);
    }
};

export const emitServerLine = (line: ServerLog) => {
    if (consoleRing.length === CONSOLE_CAP) {
        consoleRing.shift();
        consoleDropped++;
    }
    consoleRing.push(line);
    // Lazy timer: arm on the first line into an empty window; a size-cap
    // crossing preempts. No emit ⇒ no timer (the Go loop's idle quiescence).
    if (consoleTimer === null) {
        consoleTimer = setTimeout(flushConsole, CONSOLE_INTERVAL);
    } else if (consoleRing.length >= CONSOLE_BATCH_MAX) {
        clearTimeout(consoleTimer);
        flushConsole();
    }
};

export const emitServerLines = (lines: ServerLog[]) => lines.forEach(emitServerLine);

// resetConsole clears the ring between stories so transcripts don't bleed.
export const resetConsole = () => {
    if (consoleTimer !== null) {
        clearTimeout(consoleTimer);
        consoleTimer = null;
    }
    consoleRing = [];
    consoleDropped = 0;
};

const clock = (): string => {
    const d = new Date();
    const p = (n: number) => String(n).padStart(2, "0");
    return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
};

// mockServerAck fakes the MC server's response to a typed command so the
// echo round-trip reads naturally in Storybook (no real subprocess).
const mockServerAck = (cmd: string): string => {
    const info = (msg: string) => `[${clock()}] [Server thread/INFO]: ${msg}`;
    const [head, ...rest] = cmd.trim().split(/\s+/);
    switch (head) {
        case "time":
            if (rest[0] === "set") {
                const v = rest[1] === "day" ? 1000 : rest[1] === "night" ? 13000 : Number(rest[1]) || 0;
                return info(`Set the time to ${v}`);
            }
            return info("The time is 1000");
        case "say":
            return info(`[Server] ${rest.join(" ")}`);
        case "weather":
            return info(`Set the weather to ${rest[0] ?? "clear"}`);
        case "gamemode":
            return info(`Set own game mode to ${rest[0] ?? "survival"} Mode`);
        case "list":
            return info("There are 1 of a max of 20 players online: k10wl");
        case "help":
            return info("/help [<page>]");
        default:
            return info(`Unknown or incomplete command, see below for error\n${cmd}<--[HERE]`);
    }
};

// Frame sizes mirror cmd/gui/main.go window options.
const FRAMES = {
    main: { width: 560, height: 720 },
    logs: { width: 960, height: 640 },
} as const;
type FrameName = keyof typeof FRAMES;

// Frame visually mimics the Wails main window: hard pixel size, overflow
// clipped, gradient backdrop. With viewport pinned to ritualMain the iframe
// equals the frame so `100vh` resolves correctly inside ritual-app; the
// margin/shadow only become visible when the user widens the Storybook
// viewport for inspection.
const frameStyles = html`
    <style>
        :root { color-scheme: dark; }
        html, body { margin: 0; background: #0a0e14; }
        .wails-frame {
            display: block;
            margin: 0 auto;
            background: radial-gradient(1200px 600px at 20% -10%, rgba(70, 110, 200, 0.25), transparent 60%),
                radial-gradient(900px 500px at 110% 110%, rgba(180, 80, 150, 0.18), transparent 60%),
                #0f131a;
            color: #f4f4f6;
            box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.1), 0 30px 60px rgba(0, 0, 0, 0.4);
            overflow: hidden;
            position: relative;
            box-sizing: border-box;
            transform: translateZ(0);
        }
        .wails-frame > * {
            display: block;
            width: 100%;
            height: 100%;
            box-sizing: border-box;
        }
        /* Stage stories: mimic ritual-app <main> centering + max-width. */
        .wails-shell {
            display: flex;
            flex-direction: column;
            width: 100%;
            height: 100%;
        }
        .wails-main {
            flex: 1;
            min-height: 0;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 1rem;
        }
        .wails-main > * {
            width: 100%;
            max-width: 480px;
            height: auto;
        }
    </style>
`;

const preview: Preview = {
    decorators: [
        (story, ctx) => {
            const name = (ctx.parameters.window ?? "main") as FrameName;
            const { width, height } = FRAMES[name] ?? FRAMES.main;
            const bare = ctx.parameters.frame === "bare";
            const inner = bare
                ? story()
                : html`<div class="wails-shell">
                      <div class="wails-main">${story()}</div>
                  </div>`;
            return html`
                ${frameStyles}
                <div class="wails-frame" style="width:${width}px;height:${height}px;">
                    ${inner}
                </div>
            `;
        },
    ],
    parameters: {
        backgrounds: {
            default: "void",
            values: [{ name: "void", value: "#0a0e14" }],
        },
        viewport: {
            viewports: {
                ritualMain: {
                    name: "Ritual main (560×720)",
                    styles: { width: "560px", height: "720px" },
                    type: "desktop" as const,
                },
                ritualLogs: {
                    name: "Ritual logs (960×640)",
                    styles: { width: "960px", height: "640px" },
                    type: "desktop" as const,
                },
                inspect: {
                    name: "Inspect (1024×900)",
                    styles: { width: "1024px", height: "900px" },
                    type: "desktop" as const,
                },
            },
            defaultViewport: "ritualMain",
        },
    },
};

export default preview;
