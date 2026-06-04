import { css, html, LitElement } from "lit";
import { customElement, query, state } from "lit/decorators.js";
import { onServerLogs, sendConsole, type ServerLog, type ServerLogBatch } from "./wails-api";

const RING_CAPACITY = 1024;
// While scrolled up reading scrollback we DON'T trim (trimming reflows the list
// and would jitter the rows under the cursor). The backlog may grow past the
// cap until the user returns to the tail; this ceiling is the hard safety stop
// so it can't grow without bound.
const HARD_CEILING = 4096;
// Treat "within this many px of the bottom" as following the tail.
const FOLLOW_THRESHOLD = 24;
const HISTORY_MAX = 64;

// Minecraft server console (design-log/042). A simple window — the OS titlebar
// is the header, so the component is just two rows: the message stream (1fr)
// and a composer that takes only the height it needs. Output is a
// high-frequency append-only stream, not a model, so rows are appended
// imperatively into a static <ol>; Lit owns the shell, composer, empty state,
// and the "Jump to latest" pill. Tail-follow is pure CSS (`column-reverse`):
// newest is inserted just above a bottom sentinel and the view stays pinned to
// the bottom with zero JS scroll writes; an IntersectionObserver toggles the
// pill when the user scrolls up to read back.
@customElement("ritual-logs")
export class RitualLogs extends LitElement {
    @query("ol") private list!: HTMLOListElement;
    @query(".sentinel") private sentinel!: HTMLElement;
    @query(".editor") private editor!: HTMLElement;

    @state() private empty = true; // toggles the placeholder
    @state() private atBottom = true; // sentinel visible ⇒ following the tail

    private unsubscribe?: () => void;
    private io?: IntersectionObserver;
    private history: string[] = [];
    private historyCursor = -1;
    private count = 0; // live row count (excl. sentinel) for the 1024 cap
    // Rows awaiting the next frame's flush. Coalescing into one per-frame tick
    // keeps a streaming burst at a single reflow — and runs the scroll
    // compensation once — so visible rows don't jitter while scrolled up.
    private pendingNodes: Node[] = [];
    private flushScheduled = false;

    connectedCallback() {
        super.connectedCallback();
        this.unsubscribe = onServerLogs((b) => this.appendBatch(b));
        window.addEventListener("focus", this.focusEditor);
        window.addEventListener("keydown", this.onHostKey);
        document.addEventListener("visibilitychange", this.onVisibility);
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        this.unsubscribe?.();
        this.io?.disconnect();
        window.removeEventListener("focus", this.focusEditor);
        window.removeEventListener("keydown", this.onHostKey);
        document.removeEventListener("visibilitychange", this.onVisibility);
    }

    firstUpdated() {
        // Event-driven follow state — no scroll polling. threshold 0 + a bottom
        // tolerance: "at the tail" means the 1px sentinel is within 24px of the
        // bottom. A strict threshold:1 flickered the "Jump to latest" pill on
        // every appended row (sub-pixel rounding as column-reverse re-pins).
        this.io = new IntersectionObserver(
            ([e]) => (this.atBottom = e.isIntersecting),
            { root: this.list, rootMargin: "0px 0px 24px 0px", threshold: 0 },
        );
        this.io.observe(this.sentinel);
        this.focusEditor();
        this.flush(); // drain anything queued before the DOM existed
    }

    // appendBatch builds the rows and queues them; the actual DOM write happens
    // once per frame in flush() (one reflow, one scroll fix per burst).
    private appendBatch(b: ServerLogBatch) {
        if (b.dropped > 0) this.pendingNodes.push(gapRow(b.dropped));
        for (const l of b.lines) this.pendingNodes.push(renderLine(l));
        this.scheduleFlush();
    }

    private scheduleFlush() {
        if (this.flushScheduled) return;
        this.flushScheduled = true;
        // Microtask, not rAF: rAF is throttled/paused when the logs window is
        // backgrounded, which would stall the console exactly when you want it
        // to keep recording. A microtask coalesces a synchronous burst into one
        // reflow without that throttle.
        queueMicrotask(() => {
            this.flushScheduled = false;
            this.flush();
        });
    }

    // flush inserts the frame's rows just above the bottom sentinel. The list is
    // CSS `column-reverse`, so the newest must be nearest the sentinel — the
    // fragment is built in reverse and the whole burst lands in one reflow.
    // NO JS scroll writes: column-reverse pins the tail for free and keeps
    // scrollback put when scrolled up. Trimming is the only thing that can
    // jitter, so it is deferred until the user is back at the tail (where the
    // removed rows are far off-screen above and their removal is invisible).
    private flush() {
        if (!this.sentinel || this.pendingNodes.length === 0) return;
        const sc = this.list;
        const nodes = this.pendingNodes;
        this.pendingNodes = [];

        // Decide follow-vs-stay by MEASURING, synchronously, before mutating —
        // not via the IntersectionObserver (it lags a frame and its sub-pixel
        // verdict is what jittered the bottom before).
        const following = sc.scrollHeight - sc.scrollTop - sc.clientHeight <= FOLLOW_THRESHOLD;

        // Normal top→bottom order; rows land BELOW the viewport, before the
        // bottom sentinel. When scrolled up this cannot move the viewport
        // (scrollTop is anchored to the top, nothing above changes) — so reading
        // scrollback is rock-steady with ZERO scroll writes.
        const frag = document.createDocumentFragment();
        for (const n of nodes) frag.appendChild(n);
        sc.insertBefore(frag, this.sentinel);
        this.count += nodes.length;
        if (this.empty && this.count > 0) this.empty = false;

        if (following) {
            // Trim is invisible — we re-pin to the bottom right after.
            this.trimTo(RING_CAPACITY, false);
            sc.scrollTop = sc.scrollHeight;
        } else if (this.count > HARD_CEILING) {
            // Rare: bound a long scrollback session, compensating so the rows
            // under the cursor stay put.
            this.trimTo(HARD_CEILING, true);
        }
    }

    // trimTo removes oldest rows (top) down to limit. When compensate is set
    // (trimming while scrolled up), scrollTop is pulled up by exactly the
    // removed height so the visible rows don't jump.
    private trimTo(limit: number, compensate: boolean) {
        if (this.count <= limit) return;
        const sc = this.list;
        const before = compensate ? sc.scrollHeight : 0;
        while (this.count > limit) {
            const oldest = sc.firstElementChild;
            if (!oldest || oldest === this.sentinel) break;
            oldest.remove();
            this.count--;
        }
        if (compensate) {
            const shrank = before - sc.scrollHeight;
            if (shrank > 0) sc.scrollTop = Math.max(0, sc.scrollTop - shrank);
        }
    }

    private jumpToLatest = () => {
        this.list.scrollTop = this.list.scrollHeight;
    };

    private onVisibility = () => {
        if (!document.hidden) this.focusEditor();
    };

    private focusEditor = () => {
        this.editor?.focus({ preventScroll: true });
    };

    private pushHistory(text: string) {
        if (this.history[this.history.length - 1] === text) return;
        this.history.push(text);
        if (this.history.length > HISTORY_MAX) this.history.shift();
    }

    private send() {
        const text = (this.editor.textContent ?? "").trim();
        if (!text) return;
        this.pushHistory(text);
        this.historyCursor = -1;
        // Echo is backend-driven (design-log/042 §Q8): the › row arrives over
        // the wire as kind:"in" only after the stdin write is confirmed.
        void sendConsole(text);
        this.clearEditor();
    }

    private clearEditor() {
        // textContent="" can leave a stray <br>; wipe innerHTML so :empty (the
        // placeholder) matches and the box collapses back to one line.
        this.editor.innerHTML = "";
    }

    private setEditor(text: string) {
        this.editor.textContent = text;
        const range = document.createRange();
        range.selectNodeContents(this.editor);
        range.collapse(false); // caret to end
        const sel = getSelection();
        sel?.removeAllRanges();
        sel?.addRange(range);
    }

    private onEditorInput = () => {
        // Keep :empty honest so the placeholder shows after backspacing to empty.
        if ((this.editor.textContent ?? "") === "") this.clearEditor();
    };

    private onEditorKey = (e: KeyboardEvent) => {
        // Enter sends; Shift+Enter inserts a newline (the box grows).
        if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            this.send();
            return;
        }
        // History only while the draft is a single line, so multi-line editing
        // keeps native caret movement.
        const multiline = (this.editor.textContent ?? "").includes("\n");
        if (e.key === "ArrowUp" && !multiline && this.history.length > 0) {
            e.preventDefault();
            const next = this.historyCursor < 0
                ? this.history.length - 1
                : Math.max(0, this.historyCursor - 1);
            this.historyCursor = next;
            this.setEditor(this.history[next]);
            return;
        }
        if (e.key === "ArrowDown" && !multiline && this.historyCursor >= 0) {
            e.preventDefault();
            const next = this.historyCursor + 1;
            if (next >= this.history.length) {
                this.historyCursor = -1;
                this.clearEditor();
            } else {
                this.historyCursor = next;
                this.setEditor(this.history[next]);
            }
        }
    };

    private onHostKey = (e: KeyboardEvent) => {
        if (!this.editor) return;
        if (this.shadowRoot?.activeElement === this.editor) return;
        if (e.metaKey || e.ctrlKey || e.altKey) return;
        // Don't steal the caret while the user is selecting log text.
        if (!getSelection()?.isCollapsed) return;
        this.editor.focus({ preventScroll: true });
    };

    render() {
        return html`
            <div class="shell">
            <div class="viewport">
                <ol><li class="sentinel" aria-hidden="true"></li></ol>
                ${this.empty
                    ? html`<p class="empty">Server console — output appears here while the world is running.</p>`
                    : null}
                <button
                    class="jump"
                    ?hidden=${this.atBottom}
                    @click=${this.jumpToLatest}
                >
                    Jump to latest ↓
                </button>
            </div>
            <div class="composer">
                <span class="prompt" aria-hidden="true">›</span>
                <div
                    class="editor"
                    contenteditable="plaintext-only"
                    role="textbox"
                    aria-multiline="true"
                    aria-label="Server console input"
                    data-placeholder="Type a server command — Enter to send"
                    @keydown=${this.onEditorKey}
                    @input=${this.onEditorInput}
                ></div>
            </div>
            </div>
        `;
    }

    static styles = css`
        /* The grid lives on an inner shell, not :host — an embedding context
           (e.g. the Storybook frame's \`.wails-frame > *\`) can force the host's
           own display/height and would otherwise collapse the 1fr row. */
        :host {
            display: block;
            height: 100%;
            background: var(--stone-deep);
            color: var(--text);
            font-family: var(--font-mono);
            font-size: var(--fs-body);
        }
        .shell {
            display: grid;
            grid-template-rows: 1fr auto; /* messages grow; composer takes what it needs */
            height: 100%;
        }
        .viewport {
            position: relative;
            min-height: 0; /* let the 1fr row actually shrink so <ol> scrolls */
        }
        /* Normal top→bottom flow. Appends land below the viewport (before the
           bottom sentinel), so when scrolled up nothing above changes and the
           viewport can't move — scrollback is rock-steady. flush() writes
           scrollTop only while following the tail. overflow-anchor is disabled
           so the browser never adds its own (imprecise) anchoring on top. */
        ol {
            position: absolute;
            inset: 0;
            list-style: none;
            margin: 0;
            padding: var(--space-1) 0;
            overflow-y: auto;
            overflow-anchor: none;
        }
        .sentinel {
            height: 1px;
        }
        li:not(.sentinel) {
            padding: 2px var(--space-3);
            line-height: 1.4;
            white-space: pre-wrap;
            word-break: break-word;
            /* Log output is reference text — override the app-wide no-select. */
            user-select: text;
            -webkit-user-select: text;
            cursor: text;
        }
        .lvl-warn  { color: var(--state-prep); }
        .lvl-error {
            color: var(--state-fail);
            background: color-mix(in srgb, var(--state-fail) 8%, transparent);
        }
        .row-input {
            color: var(--state-run);
            background: color-mix(in srgb, var(--state-run) 10%, transparent);
        }
        .row-gap {
            color: var(--text-faint);
            text-align: center;
            font-size: var(--fs-caption);
            letter-spacing: 0.08em;
        }
        .empty {
            position: absolute;
            inset: 0;
            margin: 0;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: var(--space-6) var(--space-4);
            text-align: center;
            color: var(--text-faint);
            pointer-events: none;
        }
        .jump {
            position: absolute;
            left: 50%;
            bottom: var(--space-3);
            transform: translateX(-50%);
            padding: var(--space-1) var(--space-3);
            border: 1px solid var(--stone-bevel);
            border-radius: var(--radius-pill, 999px);
            background: var(--stone-bevel);
            color: var(--text);
            font: inherit;
            font-size: var(--fs-caption);
            cursor: pointer;
            box-shadow: 0 4px 12px rgba(0, 0, 0, 0.35);
        }
        .jump[hidden] {
            display: none;
        }
        .jump:hover {
            background: var(--stone-deep);
        }
        .composer {
            display: flex;
            align-items: flex-start;
            gap: var(--space-2);
            padding: var(--space-2) var(--space-3) var(--space-4); /* extra pad below input */
            border-top: 1px solid var(--stone-bevel);
            background: var(--stone-bevel);
        }
        .prompt {
            color: var(--state-run);
            user-select: none;
            line-height: 1.4;
        }
        .editor {
            flex: 1;
            min-width: 0;
            background: transparent;
            border: none;
            outline: none;
            color: inherit;
            font: inherit;
            line-height: 1.4;
            caret-color: var(--state-run);
            /* Grows with content; caps before it eats the message stream. */
            max-height: 8lh;
            overflow-y: auto;
            white-space: pre-wrap;
            word-break: break-word;
            user-select: text;
            -webkit-user-select: text;
        }
        .editor:empty::before {
            content: attr(data-placeholder);
            color: var(--text-faint);
            pointer-events: none;
        }
    `;
}

// renderLine builds one imperative monospace row. Output rows carry no chrome —
// MC prints its own timestamp + /INFO|WARN|ERROR] tag. Severity tint is a
// presentation concern, classified here by substring on MC's own tag; a
// backend-flagged crash (wire Level "error") always tints (design-log/042 §Q7).
function renderLine(l: ServerLog): HTMLLIElement {
    const li = document.createElement("li");
    if (l.kind === "in") {
        li.className = "row-input";
        li.textContent = "› " + l.text;
        return li;
    }
    li.className = "row-out";
    if (l.level === "error" || /\/ERROR\]/.test(l.text)) li.classList.add("lvl-error");
    else if (/\/WARN\]/.test(l.text)) li.classList.add("lvl-warn");
    li.textContent = l.text;
    return li;
}

function gapRow(n: number): HTMLLIElement {
    const li = document.createElement("li");
    li.className = "row-gap";
    li.textContent = `… ${n} line${n === 1 ? "" : "s"} dropped …`;
    return li;
}

declare global {
    interface HTMLElementTagNameMap {
        "ritual-logs": RitualLogs;
    }
}
