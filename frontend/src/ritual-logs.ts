import { css, html, LitElement } from "lit";
import { customElement, query, state } from "lit/decorators.js";
import { onLog, sendConsole, type LogLine } from "./wails-api";

const RING_CAPACITY = 500;
const HISTORY_MAX = 64;

type RowLog = { kind: "log"; id: number; line: LogLine };
type RowInput = { kind: "input"; id: number; ts: number; text: string };
type Row = RowLog | RowInput;

const CONSOLE_INPUT_PREFIX = "console input: ";

@customElement("ritual-logs")
export class RitualLogs extends LitElement {
    @state() private rows: Row[] = [];
    @state() private draft = "";
    @query(".editor") private editor!: HTMLInputElement;
    private unsubscribe?: () => void;
    private history: string[] = [];
    private historyCursor = -1;
    private rowId = 0;

    connectedCallback() {
        super.connectedCallback();
        this.unsubscribe = onLog((line) => {
            if (line.msg.startsWith(CONSOLE_INPUT_PREFIX)) {
                this.pushRow({
                    kind: "input",
                    id: this.rowId++,
                    ts: line.ts,
                    text: line.msg.slice(CONSOLE_INPUT_PREFIX.length),
                });
                return;
            }
            this.pushRow({ kind: "log", id: this.rowId++, line });
        });
        window.addEventListener("focus", this.focusEditor);
        window.addEventListener("keydown", this.onHostKey);
        document.addEventListener("visibilitychange", this.onVisibility);
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        this.unsubscribe?.();
        window.removeEventListener("focus", this.focusEditor);
        window.removeEventListener("keydown", this.onHostKey);
        document.removeEventListener("visibilitychange", this.onVisibility);
    }

    private onVisibility = () => {
        if (!document.hidden) this.focusEditor();
    };

    firstUpdated() {
        this.focusEditor();
    }

    updated() {
        const wrap = this.shadowRoot?.querySelector<HTMLElement>(".wrap");
        if (wrap) wrap.scrollTop = wrap.scrollHeight;
    }

    private pushRow(row: Row) {
        const next = this.rows.length >= RING_CAPACITY ? this.rows.slice(1) : this.rows.slice();
        next.push(row);
        this.rows = next;
    }

    private focusEditor = () => {
        this.editor?.focus({ preventScroll: true });
    };

    private pushHistory(text: string) {
        if (this.history[this.history.length - 1] === text) return;
        this.history.push(text);
        if (this.history.length > HISTORY_MAX) this.history.shift();
    }

    private send(text: string) {
        this.pushHistory(text);
        this.historyCursor = -1;
        void sendConsole(text);
        this.draft = "";
    }

    private onInput = (e: InputEvent) => {
        this.draft = (e.target as HTMLInputElement).value;
    };

    private onEditorKey = (e: KeyboardEvent) => {
        if (e.key === "Enter") {
            e.preventDefault();
            const text = this.draft.trim();
            if (!text) return;
            this.send(text);
            return;
        }
        if (e.key === "ArrowUp" && this.history.length > 0) {
            e.preventDefault();
            const next = this.historyCursor < 0
                ? this.history.length - 1
                : Math.max(0, this.historyCursor - 1);
            this.historyCursor = next;
            this.draft = this.history[next];
            return;
        }
        if (e.key === "ArrowDown" && this.historyCursor >= 0) {
            e.preventDefault();
            const next = this.historyCursor + 1;
            if (next >= this.history.length) {
                this.historyCursor = -1;
                this.draft = "";
            } else {
                this.historyCursor = next;
                this.draft = this.history[next];
            }
        }
    };

    private onHostKey = (e: KeyboardEvent) => {
        if (!this.editor) return;
        if (this.shadowRoot?.activeElement === this.editor) return;
        if (e.metaKey || e.ctrlKey || e.altKey) return;
        this.editor.focus({ preventScroll: true });
    };

    render() {
        return html`
            <header>
                <span class="title">Logs</span>
                <span class="count">${this.rows.length}</span>
            </header>
            <div class="wrap">
                ${this.rows.length === 0
                    ? html`<p class="empty">No events yet. This console mirrors the Ritual event bus.</p>`
                    : html`
                          <ol>
                              ${this.rows.map((r) =>
                                  r.kind === "log"
                                      ? html`
                                            <li class=${"lvl-" + r.line.level}>
                                                <time>${formatTs(r.line.ts)}</time>
                                                <span class="level">${r.line.level}</span>
                                                <span class="msg">${r.line.msg}</span>
                                            </li>
                                        `
                                      : html`
                                            <li class="row-input">
                                                <time>${formatTs(r.ts)}</time>
                                                <span class="level">›</span>
                                                <span class="msg">${r.text}</span>
                                            </li>
                                        `,
                              )}
                          </ol>
                      `}
            </div>
            <footer>
                <span class="prompt" aria-hidden="true">›</span>
                <input
                    class="editor"
                    type="text"
                    autocomplete="off"
                    autocorrect="off"
                    autocapitalize="off"
                    spellcheck="false"
                    aria-label="Server console input"
                    placeholder="Type a server command — Enter to send"
                    .value=${this.draft}
                    @input=${this.onInput}
                    @keydown=${this.onEditorKey}
                    autofocus
                />
            </footer>
        `;
    }

    static styles = css`
        :host {
            display: flex;
            flex-direction: column;
            height: 100vh;
            background: #0b0e13;
            color: #dcdfe4;
            font-family: "Departure Mono", monospace;
            font-size: 0.85rem;
        }
        header {
            display: flex;
            align-items: center;
            justify-content: space-between;
            padding: 0.5rem 0.9rem;
            border-bottom: 1px solid rgba(255, 255, 255, 0.06);
            background: rgba(255, 255, 255, 0.02);
        }
        .title {
            font-size: 0.72rem;
            text-transform: uppercase;
            letter-spacing: 0.14em;
            opacity: 0.55;
        }
        .count {
            font-variant-numeric: tabular-nums;
            opacity: 0.45;
            font-size: 0.72rem;
        }
        .wrap {
            flex: 1;
            overflow-y: auto;
            padding: 0.35rem 0;
        }
        ol {
            list-style: none;
            margin: 0;
            padding: 0;
        }
        li {
            display: grid;
            grid-template-columns: 8ch 5ch 1fr;
            gap: 0.75rem;
            padding: 0.18rem 0.9rem;
            line-height: 1.35;
        }
        li:nth-child(odd) {
            background: rgba(255, 255, 255, 0.012);
        }
        time {
            opacity: 0.45;
        }
        .level {
            text-transform: uppercase;
            font-size: 0.7rem;
            letter-spacing: 0.05em;
            align-self: center;
        }
        .lvl-info .level { color: #7ea8ff; }
        .lvl-warn .level { color: #ffc876; }
        .lvl-error .level { color: #ff9e9e; }
        .lvl-error { background: rgba(255, 80, 80, 0.08); }
        .row-input {
            background: rgba(90, 180, 120, 0.10);
        }
        .row-input .level {
            color: #8fe3a4;
            font-size: 1rem;
            text-transform: none;
        }
        .row-input .msg {
            color: #caf0d2;
        }
        .msg {
            white-space: pre-wrap;
            word-break: break-word;
        }
        .empty {
            padding: 2rem 1rem;
            text-align: center;
            opacity: 0.6;
        }
        footer {
            display: flex;
            align-items: center;
            gap: 0.6rem;
            padding: 0.5rem 0.9rem;
            border-top: 1px solid rgba(255, 255, 255, 0.06);
            background: rgba(255, 255, 255, 0.025);
        }
        .prompt {
            opacity: 0.75;
            color: #8fe3a4;
            user-select: none;
        }
        .editor {
            flex: 1;
            background: transparent;
            border: none;
            outline: none;
            color: inherit;
            font: inherit;
            caret-color: #8fe3a4;
            padding: 0;
        }
        .editor::placeholder {
            opacity: 0.35;
        }
    `;
}

function formatTs(ms: number): string {
    const d = new Date(ms);
    const pad = (n: number) => String(n).padStart(2, "0");
    return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

declare global {
    interface HTMLElementTagNameMap {
        "ritual-logs": RitualLogs;
    }
}
