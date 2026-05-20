import { LitElement, css, html, PropertyValues } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { gsap } from "gsap";

const EDGE_PAD = 2;
const SETTLE_AT = 0.3;
const PULSE_MIN_S = 3.0;
const PULSE_MAX_S = 6.0;
const PULSE_DURATION_S = 0.4;

type Slot = { from: string; to: string; active: boolean; cached: string };

const reducedMotion = (): boolean =>
    window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;

const rand = (n: number): number => Math.floor(Math.random() * n);
const isWS = (c: string): boolean => c !== "" && /\s/.test(c);

const GLYPH_RANGES: ReadonlyArray<readonly [number, number]> = [
    [0x21, 94],     // printable ASCII
    [0x2500, 256],  // box drawing + block + geometric shapes
    [0x2190, 112],  // arrows
    [0x2200, 256],  // math operators
];
const randGlyph = (): string => {
    const [start, len] = GLYPH_RANGES[(Math.random() * GLYPH_RANGES.length) | 0];
    return String.fromCharCode(start + ((Math.random() * len) | 0));
};

const findDiffWindow = (from: string, to: string): { lo: number; hi: number } | null => {
    const len = Math.max(from.length, to.length);
    let lo = 0;
    while (lo < len && (from[lo] ?? "") === (to[lo] ?? "")) lo++;
    if (lo === len) return null;
    let hi = len - 1;
    while (hi >= lo) {
        const fi = from.length - (len - hi);
        const ti = to.length - (len - hi);
        if ((fi >= 0 ? from[fi] : "") !== (ti >= 0 ? to[ti] : "")) break;
        hi--;
    }
    return {
        lo: Math.max(0, lo - EDGE_PAD),
        hi: Math.min(len - 1, hi + EDGE_PAD),
    };
};

const buildSlots = (from: string, to: string, w: { lo: number; hi: number }): Slot[] => {
    const len = Math.max(from.length, to.length);
    const slots: Slot[] = [];
    for (let i = 0; i < len; i++) {
        const f = from[i] ?? "";
        const t = to[i] ?? "";
        const active = i >= w.lo && i <= w.hi && !isWS(f) && !isWS(t);
        slots.push({ from: f, to: t, active, cached: "" });
    }
    return slots;
};

@customElement("decoder-text")
export class DecoderText extends LitElement {
    @property() text = "";
    @property({ type: Number }) duration = 0.8;

    @state() private display = "";
    private settled = "";
    private progress = { v: 0 };
    private tween?: gsap.core.Tween;
    private pulseTimer = 0;

    connectedCallback() {
        super.connectedCallback();
        this.schedulePulse();
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        this.tween?.kill();
        clearTimeout(this.pulseTimer);
    }

    updated(changed: PropertyValues) {
        if (!changed.has("text")) return;
        if (this.text === this.settled && !this.tween?.isActive()) return;
        this.scramble(this.text);
    }

    private scramble(
        to: string,
        opts?: { window?: { lo: number; hi: number }; duration?: number },
    ) {
        clearTimeout(this.pulseTimer);
        const from = this.display || this.settled;
        if (reducedMotion()) {
            this.commit(to);
            return;
        }
        const window = opts?.window ?? findDiffWindow(from, to);
        if (!window) {
            this.commit(to);
            return;
        }
        const slots = buildSlots(from, to, window);
        const fLen = from.length;
        const tLen = to.length;
        this.tween?.kill();
        this.progress.v = 0;
        this.tween = gsap.to(this.progress, {
            v: 1,
            duration: opts?.duration ?? this.duration,
            ease: "none",
            onUpdate: () => {
                const p = this.progress.v;
                const visibleLen = fLen + Math.round((tLen - fLen) * p);
                let out = "";
                for (let i = 0; i < slots.length; i++) {
                    if (i >= visibleLen) continue;
                    const s = slots[i];
                    if (!s.active) {
                        out += s.to !== "" ? s.to : s.from;
                        continue;
                    }
                    if (p >= SETTLE_AT) {
                        out += s.to;
                        continue;
                    }
                    if (!s.cached || Math.random() < 0.5) s.cached = randGlyph();
                    out += s.cached;
                }
                this.display = out;
            },
            onComplete: () => this.commit(to),
        });
    }

    private commit(text: string) {
        this.display = text;
        this.settled = text;
        this.schedulePulse();
    }

    private schedulePulse() {
        clearTimeout(this.pulseTimer);
        if (reducedMotion()) return;
        const delay = (PULSE_MIN_S + Math.random() * (PULSE_MAX_S - PULSE_MIN_S)) * 1000;
        this.pulseTimer = window.setTimeout(() => this.pulse(), delay);
    }

    private pulse() {
        if (this.tween?.isActive() || !this.settled.length) {
            this.schedulePulse();
            return;
        }
        const base = this.settled;
        const center = rand(base.length);
        this.scramble(base, {
            window: {
                lo: Math.max(0, center - 1 - EDGE_PAD),
                hi: Math.min(base.length - 1, center + 1 + EDGE_PAD),
            },
            duration: PULSE_DURATION_S,
        });
    }

    render() {
        return html`<span>${this.display || this.text}</span>`;
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
        "decoder-text": DecoderText;
    }
}
