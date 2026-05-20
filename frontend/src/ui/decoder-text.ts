import { LitElement, css, html, PropertyValues } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { gsap } from "gsap";

const DEFAULT_CHARS =
    "!<>-_\\/[]{}()=+*^?#@$%&|~`:;,.\"'" +
    "0123456789" +
    "ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
    "abcdefghijklmnopqrstuvwxyz" +
    "█▓▒░▄▀■□▪▫◆◇◢◣◤◥●○◐◑◒◓" +
    "←→↑↓↔↕⇄⇅⇆⇇⇈⇉⇋⇌" +
    "±×÷≈≠≤≥∞∂∆∇∑∏∫√" +
    "¢£¥€₿§¶©®™" +
    "アイウエオカキクケコサシスセソタチツテトナニヌネノハヒフヘホマミムメモヤユヨラリルレロワヲン";

const FRAMES_TOTAL = 80;
const SCRAMBLE_MAX = 36;

type Slot = { from: string; to: string; start: number; end: number; char: string };

const reducedMotion = (): boolean =>
    window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;

const rand = (n: number): number => Math.floor(Math.random() * n);

const buildSlots = (from: string, to: string): Slot[] => {
    const len = Math.max(from.length, to.length);
    const slots: Slot[] = [];
    for (let i = 0; i < len; i++) {
        const f = from[i] ?? "";
        const t = to[i] ?? "";
        if (f === t) {
            slots.push({ from: f, to: t, start: 0, end: 0, char: "" });
            continue;
        }
        const start = rand(SCRAMBLE_MAX);
        slots.push({
            from: f,
            to: t,
            start,
            end: start + SCRAMBLE_MAX + rand(SCRAMBLE_MAX),
            char: "",
        });
    }
    return slots;
};

@customElement("decoder-text")
export class DecoderText extends LitElement {
    @property() text = "";
    @property({ type: Number }) duration = 0.8;
    @property() chars = DEFAULT_CHARS;

    @state() private display = "";
    private prev = "";
    private progress = { v: 0 };
    private tween?: gsap.core.Tween;

    updated(changed: PropertyValues) {
        if (!changed.has("text")) return;
        if (this.text === this.prev) return;
        this.scramble(this.prev, this.text);
        this.prev = this.text;
    }

    private scramble(from: string, to: string) {
        if (reducedMotion()) {
            this.display = to;
            return;
        }
        const slots = buildSlots(from, to);
        const total = FRAMES_TOTAL + SCRAMBLE_MAX;
        this.tween?.kill();
        this.progress.v = 0;
        this.tween = gsap.to(this.progress, {
            v: 1,
            duration: this.duration,
            ease: "none",
            onUpdate: () => {
                const now = this.progress.v * total;
                let out = "";
                for (const s of slots) {
                    if (now >= s.end) {
                        out += s.to;
                    } else if (now >= s.start) {
                        if (!s.char || Math.random() < 0.3) {
                            s.char = this.chars[rand(this.chars.length)];
                        }
                        out += s.char;
                    } else {
                        out += s.from;
                    }
                }
                this.display = out;
            },
            onComplete: () => { this.display = to; },
        });
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        this.tween?.kill();
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
