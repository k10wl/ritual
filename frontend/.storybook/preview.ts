import type { Preview } from "@storybook/web-components-vite";
import { html } from "lit";
import { setTransport } from "@wailsio/runtime";
import { JoinAddress, Stage, ViewModel } from "../src/wails-api";

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
    Retry: 1907515874,
    SendConsole: 344753175,
    ShowLogs: 3352242658,
    Start: 4262819292,
    Stop: 4133576568,
};
const OBJ_CALL = 0;
const OBJ_EVENTS = 3;

const fixtures = {
    idle: () => new ViewModel({ stage: Stage.StageIdle }),
    locked: () =>
        new ViewModel({ stage: Stage.StageLocked, lockHolder: "alice" }),
    downloading: (progress: number) =>
        new ViewModel({
            stage: Stage.StageDownloading,
            progress,
            bytesDone: progress * 10_000_000,
            bytesTotal: 1_000_000_000,
            speedMbps: 32,
            label: "Downloading world…",
        }),
    running: () =>
        new ViewModel({
            stage: Stage.StageRunning,
            readyLight: true,
            addresses: [
                new JoinAddress({ label: "LAN", address: "192.168.1.10:25565" }),
                new JoinAddress({ label: "Tailscale", address: "100.64.0.5:25565" }),
            ],
        }),
    uploading: (progress: number) =>
        new ViewModel({
            stage: Stage.StageUploading,
            progress,
            bytesDone: progress * 10_000_000,
            bytesTotal: 1_000_000_000,
            speedMbps: 22,
            label: "Uploading world…",
        }),
};

let current: ViewModel = fixtures.idle();
let timer: ReturnType<typeof setInterval> | null = null;

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
                ramp(fixtures.downloading, () => set(fixtures.running()));
                return undefined;
            case M.Stop:
                ramp(fixtures.uploading, () => set(fixtures.idle()));
                return undefined;
            case M.Retry:
                set(fixtures.idle());
                return undefined;
            default:
                return undefined;
        }
    },
});

export const pushView = (vm: unknown) =>
    window._wails?.dispatchWailsEvent({ name: "ritual:view", data: vm });

export const pushLog = (line: unknown) =>
    window._wails?.dispatchWailsEvent({ name: "log:line", data: line });

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
