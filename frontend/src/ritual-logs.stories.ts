import { html, LitElement } from "lit";
import { customElement, property, query } from "lit/decorators.js";
import "./ritual-logs";
import type { RitualLogs } from "./ritual-logs";
import { Level, type ServerLog } from "./wails-api";
import { emitServerLine, resetConsole } from "../.storybook/preview";

export default {
    title: "Screens / Server Console",
    component: "ritual-logs",
    parameters: {
        frame: "bare",
        window: "logs",
        docs: {
            description: {
                component:
                    "Minecraft server console (design-log/042). Stories feed lines into a Storybook " +
                    "ring-buffer engine that mirrors the Go batchingLogEmitter — it coalesces them into " +
                    "`server:logs` batches just like the backend. CSS `column-reverse` tail-follow (no JS " +
                    "scroll writes); raw selectable monospace rows with frontend WARN/ERROR tint; the " +
                    "contenteditable composer echoes typed commands back as `›` rows over the wire.",
            },
        },
    },
};

// --- fixtures ---------------------------------------------------------------

type Sev = "INFO" | "WARN" | "ERROR";

const mc = (time: string, text: string, sev: Sev = "INFO"): ServerLog => ({
    ts: Date.now(),
    kind: "out",
    level: Level.$zero,
    text: `[${time}] [Server thread/${sev}]: ${text}`,
});

const crash = (text: string): ServerLog => ({ ts: Date.now(), kind: "out", level: Level.LevelError, text });
const input = (text: string): ServerLog => ({ ts: Date.now(), kind: "in", level: Level.$zero, text });

// A realistic boot → gameplay transcript, padded to ~200 lines so the stream
// overflows the window and exercises tail-follow.
const TRANSCRIPT: ServerLog[] = (() => {
    const out: ServerLog[] = [
        mc("14:02:03", "Starting minecraft server version 1.21.4"),
        mc("14:02:03", "Loading properties"),
        mc("14:02:03", "Default game type: SURVIVAL"),
        mc("14:02:04", "Generating keypair"),
        mc("14:02:04", "Starting Minecraft server on *:25565"),
        mc("14:02:05", "Using epoll channel type"),
        mc("14:02:05", "Preparing level \"world\""),
        mc("14:02:08", "Preparing start region for dimension minecraft:overworld"),
        mc("14:02:09", "Preparing spawn area: 24%"),
        mc("14:02:10", "Preparing spawn area: 68%"),
        mc("14:02:11", "Time elapsed: 3204 ms"),
        mc("14:02:11", "Done (5.214s)! For help, type \"help\""),
    ];
    const players = ["k10wl", "steve", "alex", "notch"];
    let h = 14, m = 3;
    for (let i = 0; i < 190; i++) {
        m++;
        if (m >= 60) { m = 0; h++; }
        const t = `${String(h).padStart(2, "0")}:${String(m).padStart(2, "0")}:${String(i % 60).padStart(2, "0")}`;
        const p = players[i % players.length];
        const r = i % 17;
        if (r === 5) out.push(mc(t, `Can't keep up! Is the server overloaded? Running ${2000 + i}ms behind`, "WARN"));
        else if (r === 11) out.push(mc(t, `Failed to load chunk at ${i},${i}`, "ERROR"));
        else if (r === 3) out.push(mc(t, `${p} joined the game`));
        else if (r === 7) out.push(mc(t, `${p} left the game`));
        else if (r === 9) out.push(mc(t, `<${p}> gg this place is huge`));
        else out.push(mc(t, `[${p}: Set the time to ${i * 1000}]`));
    }
    return out;
})();

// nextLine generates an endless stream of realistic MC lines so `live` stories
// keep feeding the ring buffer forever (exercising the 500-row trim + the
// engine's drop-oldest under sustained load).
const PLAYERS = ["k10wl", "steve", "alex", "notch", "herobrine"];
let genSeq = 0;
const nextLine = (): ServerLog => {
    const i = genSeq++;
    const p2 = (n: number) => String(n).padStart(2, "0");
    const t = `${p2((14 + Math.floor(i / 3600)) % 24)}:${p2(Math.floor(i / 60) % 60)}:${p2(i % 60)}`;
    const p = PLAYERS[i % PLAYERS.length];
    switch (i % 13) {
        case 0: return mc(t, `${p} joined the game`);
        case 4: return mc(t, `<${p}> anyone got spare diamonds?`);
        case 6: return mc(t, `Can't keep up! Is the server overloaded? Running ${1500 + (i % 900)}ms behind`, "WARN");
        case 9: return mc(t, `${p} has made the advancement [Stone Age]`);
        case 11: return mc(t, `Failed to load chunk at ${i % 64},${(i * 7) % 64}`, "ERROR");
        case 12: return mc(t, `${p} left the game`);
        default: return mc(t, `[${p}: Set own game mode to Survival Mode]`);
    }
};

// --- driver -----------------------------------------------------------------

// Feeds lines into the Storybook ring-buffer engine in small spurts after
// mount, the way scanned stdout reaches the Go emitter at runtime. The engine
// (preview.ts) coalesces them into server:logs batches. `live` keeps emitting
// forever (generated lines after the seed runs out); `scrollUp` reaches into
// the console once to demonstrate the no-hijack tail-follow + pill.
@customElement("logs-driver")
export class LogsDriver extends LitElement {
    @property({ attribute: false }) lines: ServerLog[] = [];
    @property({ type: Boolean }) live = false;
    @property({ type: Boolean }) scrollUp = false;
    @query("ritual-logs") private logs!: RitualLogs;
    private timer = 0;
    private scrolled = false;

    connectedCallback() {
        super.connectedCallback();
        resetConsole();
        let i = 0;
        const tick = () => {
            const spurt = this.live ? 4 : 5;
            for (let k = 0; k < spurt; k++) {
                if (i < this.lines.length) emitServerLine(this.lines[i++]);
                else if (this.live) emitServerLine(nextLine());
                else break;
            }
            if (this.scrollUp && i >= 40 && !this.scrolled) {
                this.scrolled = true;
                // column-reverse: the bottom is scrollTop 0, so up is negative.
                requestAnimationFrame(() => {
                    const ol = this.logs?.shadowRoot?.querySelector("ol");
                    if (ol) ol.scrollTop = -240;
                });
            }
            if (this.live || i < this.lines.length) {
                this.timer = window.setTimeout(tick, this.live ? 110 : 60);
            }
        };
        this.timer = window.setTimeout(tick, 60);
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        clearTimeout(this.timer);
    }

    render() {
        return html`<ritual-logs></ritual-logs>`;
    }
}

// --- stories ----------------------------------------------------------------

// Empty: the world is not running yet — only the placeholder copy. Type in the
// composer to watch the wire-driven echo (preview.ts mocks the server ack).
export const Empty = () => {
    resetConsole();
    return html`<ritual-logs></ritual-logs>`;
};

// Streaming: boots, then streams forever through the engine — an infinite ring
// buffered feed. The view stays pinned to the newest line via CSS (no JS scroll
// writes) and the DOM holds steady at the 500-row cap as lines keep coming.
export const Streaming = () => html`<logs-driver .lines=${TRANSCRIPT} live></logs-driver>`;

// ScrolledUp: output keeps arriving forever while the user is scrolled back —
// the view does NOT yank to the bottom and the "Jump to latest ↓" pill appears.
export const ScrolledUp = () => html`<logs-driver .lines=${TRANSCRIPT} live scrollUp></logs-driver>`;

// Crash: a backend-flagged crash line (wire Level "error") is always tinted,
// independent of MC's own tags.
export const Crash = () =>
    html`<logs-driver
        .lines=${[
            ...TRANSCRIPT.slice(0, 30),
            crash("server crashed: exit status 1 (java.lang.OutOfMemoryError)"),
        ]}
    ></logs-driver>`;

// InputEcho: a typed command echoes back as a › input row (kind:"in") and the
// server's response follows as normal output.
export const InputEcho = () =>
    html`<logs-driver
        .lines=${[
            ...TRANSCRIPT.slice(0, 18),
            input("time set day"),
            mc("14:09:12", "Set the time to 1000"),
            input("say hello world"),
            mc("14:09:20", "[Server] hello world"),
        ]}
    ></logs-driver>`;

declare global {
    interface HTMLElementTagNameMap {
        "logs-driver": LogsDriver;
    }
}
