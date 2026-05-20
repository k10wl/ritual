import { LitElement, css, html, PropertyValues } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { Cell } from "./cell";
import { diffMatrix, EditGroup, groupEdits } from "./diff";
import { GlyphSource, RangeGlyphSource } from "./glyphs";
import { Ripple, RippleSpec } from "./ripple";
import { Rng, SeededRng, cryptoSeed } from "./rng";
import { Scheduler } from "./scheduler";

export * from "./cell";
export * from "./diff";
export * from "./glyphs";
export * from "./ripple";
export * from "./rng";
export * from "./scheduler";

const reducedMotion = (): boolean =>
    window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;

export type Rounds = number | readonly [number, number];

@customElement("decoder-v2")
export class DecoderV2 extends LitElement {
    @property() text = "";
    @property({ type: Number }) seed: number | null = null;

    @property({ type: Number, attribute: "splash-count" }) splashCount = 1;
    @property({ type: Number, attribute: "splash-radius" }) splashRadius = 3;
    @property({ attribute: false }) splashRounds: Rounds = [4, 8];
    @property({ type: Number, attribute: "splash-tick-ms" }) splashTickMs = 50;

    @property({ type: Number, attribute: "idle-min-ms" }) idleMinMs = 2000;
    @property({ type: Number, attribute: "idle-max-ms" }) idleMaxMs = 5000;
    @property({ type: Number, attribute: "idle-radius" }) idleRadius = 1;
    @property({ attribute: false }) idleRounds: Rounds = [2, 4];
    @property({ type: Number, attribute: "idle-tick-ms" }) idleTickMs = 80;

    @property({ attribute: false }) seedTransform: (text: string) => string = () => "";

    @state() private display = "";

    private cells: Cell[] = [];
    private rng!: Rng;
    private glyphs!: GlyphSource;
    private scheduler!: Scheduler;
    private idleTimer = 0;
    private settledText = "";
    private lastEmittedSettle = "";
    private inited = false;

    connectedCallback() {
        super.connectedCallback();
        this.rng = new SeededRng(this.seed ?? cryptoSeed());
        this.glyphs = new RangeGlyphSource(this.rng);
        this.scheduler = new Scheduler(() => this.renderTick());
        this.scheduleIdle();
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        this.scheduler.stop();
        clearTimeout(this.idleTimer);
    }

    updated(changed: PropertyValues) {
        if (!changed.has("text")) return;
        if (!this.inited) {
            this.settledText = this.seedTransform(this.text);
            this.cells = Array.from(this.settledText, (ch) => new Cell(ch));
            this.display = this.settledText;
            this.inited = true;
        }
        this.applyText(this.text);
    }

    private applyText(next: string) {
        if (this.settledText === next) return;
        if (reducedMotion()) {
            this.cells = Array.from(next, (ch) => new Cell(ch));
            this.settledText = next;
            this.display = next;
            this.emitSettled();
            return;
        }
        // Invariant for the diff that follows: this.cells aligned 1:1 with
        // settledText. Drop any "dying" cells (target === "") left over from
        // previous delete groups so oldIdx → this.cells[oldIdx] is correct.
        this.cells = this.cells.filter((c) => c.target !== "");
        const prev = this.settledText;
        const groups = groupEdits(diffMatrix(prev, next));

        const built: Cell[] = [];
        const rippleRanges: Array<{ lo: number; hi: number; op: EditGroup["op"] }> = [];

        for (const g of groups) {
            const startPos = built.length;
            this.applyGroup(g, built);
            if (g.op !== "match") {
                rippleRanges.push({ lo: startPos, hi: built.length - 1, op: g.op });
            }
        }

        this.cells = built;

        for (const r of rippleRanges) this.spawnRipple(r.lo, r.hi, r.op);

        this.settledText = next;
        this.dispatchEvent(new CustomEvent("text-targeted", { detail: { text: next } }));
        this.renderTick();
    }

    private applyGroup(g: EditGroup, out: Cell[]) {
        if (g.op === "match") {
            for (const e of g.edits) {
                const cell = this.cells[e.oldIdx];
                if (cell) out.push(cell);
            }
            return;
        }
        if (g.op === "replace") {
            for (const e of g.edits) {
                const cell = this.cells[e.oldIdx];
                if (!cell) {
                    out.push(new Cell(e.ch, /\s/.test(e.ch) ? e.ch : ""));
                    continue;
                }
                cell.retarget(e.ch);
                out.push(cell);
            }
            return;
        }
        if (g.op === "insert") {
            for (const e of g.edits) {
                const isWS = /\s/.test(e.ch);
                out.push(new Cell(e.ch, isWS ? e.ch : ""));
            }
            return;
        }
        // delete
        for (const e of g.edits) {
            const cell = this.cells[e.oldIdx];
            if (!cell) continue;
            cell.retarget("");
            out.push(cell);
        }
    }

    private spawnRipple(lo: number, hi: number, op: EditGroup["op"]) {
        const center = Math.floor((lo + hi) / 2);
        const span = Math.ceil((hi - lo) / 2);
        const isLengthChange = op === "insert" || op === "delete";
        const radius = isLengthChange ? span : span + this.splashRadius;
        for (let i = 0; i < this.splashCount; i++) {
            this.spawn({
                center,
                radius,
                rounds: this.splashRounds,
                tickDurationMs: this.splashTickMs,
                lengthRush: isLengthChange,
            });
        }
        this.dispatchEvent(
            new CustomEvent("splash-start", { detail: { center, radius, count: this.splashCount, op } }),
        );
    }

    private spawn(spec: RippleSpec) {
        this.scheduler.add(
            new Ripple(spec, this.cells, this.rng, this.glyphs, performance.now()),
        );
        this.dispatchEvent(new CustomEvent("ripple-start", { detail: spec }));
    }

    private scheduleIdle() {
        clearTimeout(this.idleTimer);
        if (reducedMotion()) return;
        const delay = this.rng?.range(this.idleMinMs, this.idleMaxMs) ?? this.idleMinMs;
        this.idleTimer = window.setTimeout(() => this.idleTick(), delay);
    }

    private idleTick() {
        if (this.cells.length) {
            this.spawn({
                center: this.rng.int(this.cells.length),
                radius: this.idleRadius,
                rounds: this.idleRounds,
                tickDurationMs: this.idleTickMs,
            });
        }
        this.scheduleIdle();
    }

    private renderTick() {
        for (let i = this.cells.length - 1; i >= 0; i--) {
            const c = this.cells[i];
            if (c.target === "" && !c.scrambling) this.cells.splice(i, 1);
        }
        this.display = this.cells.map((c) => c.glyph).join("");
        if (
            this.display === this.settledText &&
            this.cells.every((c) => !c.scrambling)
        ) {
            this.emitSettled();
        }
    }

    private emitSettled() {
        if (this.lastEmittedSettle === this.settledText) return;
        this.lastEmittedSettle = this.settledText;
        this.dispatchEvent(
            new CustomEvent("text-settled", { detail: { text: this.settledText } }),
        );
    }

    render() {
        return html`<span>${this.display}</span>`;
    }

    static styles = css`
        :host {
            display: inline-block;
            font-family: ui-monospace, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace;
            font-variant-numeric: tabular-nums;
            white-space: pre-wrap;
            overflow-wrap: anywhere;
            color: inherit;
        }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        "decoder-v2": DecoderV2;
    }
}
