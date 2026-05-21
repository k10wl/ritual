import { LitElement, css, html } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { gsap } from "gsap";
import { MorphSVGPlugin } from "gsap/MorphSVGPlugin";
import { Copy, Check } from "lucide";
import svgpath from "svgpath";
import "./decoder-v2";
import type { JoinAddress } from "../wails-api";

gsap.registerPlugin(MorphSVGPlugin);

const BOUNCE_S = 0.14;
const ICON_MORPH_S = 0.22;
const BREATH_MS = 1000;
const ICON_MORPH_MS = ICON_MORPH_S * 1000;
const MORPH_BACK_DELAY_MS = BREATH_MS - ICON_MORPH_MS;
const ROW_ENTER_S = 0.36;
const ROW_EXIT_S = 0.28;
const ROW_STAGGER_S = 0.055;
const ROW_SLIDE_PX = 12;
const STAGGER_SLOTS = 4;
export const RUN_ADDRESSES_EXIT_TOTAL_S = ROW_EXIT_S + ROW_STAGGER_S * STAGGER_SLOTS;

const reducedMotion = (): boolean =>
    window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;

type LucideChild = readonly [string, Record<string, string>];
type LucideIcon = ReadonlyArray<LucideChild>;

const shapeToD = ([tag, a]: LucideChild): string => {
    if (tag === "path") return a.d;
    if (tag === "line") return `M${a.x1} ${a.y1}L${a.x2} ${a.y2}`;
    if (tag === "polyline") {
        const pts = a.points.trim().split(/\s+/);
        return pts.map((p, i) => (i === 0 ? `M${p}` : `L${p}`)).join("");
    }
    if (tag === "rect") {
        const x = +a.x, y = +a.y, w = +a.width, h = +a.height;
        const r = +(a.rx ?? a.ry ?? 0);
        if (!r) return `M${x} ${y}h${w}v${h}h${-w}Z`;
        return `M${x + r} ${y}H${x + w - r}A${r} ${r} 0 0 1 ${x + w} ${y + r}` +
               `V${y + h - r}A${r} ${r} 0 0 1 ${x + w - r} ${y + h}` +
               `H${x + r}A${r} ${r} 0 0 1 ${x} ${y + h - r}` +
               `V${y + r}A${r} ${r} 0 0 1 ${x + r} ${y}Z`;
    }
    return "";
};

const compoundD = (icon: LucideIcon): string =>
    icon.map(shapeToD).filter(Boolean).map((d) => svgpath(d).abs().toString()).join(" ");

const D_COPY = compoundD(Copy as LucideIcon);
const D_CHECK = compoundD(Check as LucideIcon);

@customElement("run-addresses")
export class RunAddresses extends LitElement {
    @property({ attribute: false }) addresses: JoinAddress[] = [];

    @state() private copiedIndex: number | null = null;
    private morphBackTimer = 0;
    private resetTimer = 0;

    private clearTimers() {
        if (this.morphBackTimer) {
            clearTimeout(this.morphBackTimer);
            this.morphBackTimer = 0;
        }
        if (this.resetTimer) {
            clearTimeout(this.resetTimer);
            this.resetTimer = 0;
        }
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        this.clearTimers();
    }

    firstUpdated() {
        this.playEnter();
    }

    private staggerTargets(): Element[] {
        const root = this.shadowRoot;
        if (!root) return [];
        return [...root.querySelectorAll(".row")];
    }

    private playEnter() {
        if (reducedMotion()) return;
        const targets = this.staggerTargets();
        if (!targets.length) return;
        gsap.from(targets, {
            y: ROW_SLIDE_PX,
            opacity: 0,
            duration: ROW_ENTER_S,
            ease: "back.out(1.4)",
            stagger: ROW_STAGGER_S,
            overwrite: true,
        });
    }

    playExit(): gsap.core.Tween | undefined {
        if (reducedMotion()) return undefined;
        const targets = this.staggerTargets();
        if (!targets.length) return undefined;
        return gsap.to(targets, {
            y: ROW_SLIDE_PX,
            opacity: 0,
            duration: ROW_EXIT_S,
            ease: "power2.in",
            stagger: { each: ROW_STAGGER_S, from: "end" },
            overwrite: true,
        });
    }

    private iconPathAt(index: number): SVGPathElement | null {
        const root = this.shadowRoot;
        if (!root) return null;
        return root.querySelector(`.row[data-index="${index}"] .icon-path`) as SVGPathElement | null;
    }

    private morphIcon(index: number, to: "copy" | "check", instant: boolean) {
        const el = this.iconPathAt(index);
        if (!el) return;
        const targetD = to === "check" ? D_CHECK : D_COPY;
        if (instant || reducedMotion()) {
            gsap.killTweensOf(el);
            el.setAttribute("d", targetD);
            return;
        }
        gsap.to(el, { duration: ICON_MORPH_S, ease: "power2.inOut", morphSVG: targetD, overwrite: true });
    }

    private async onActivate(index: number, e: Event) {
        const addr = this.addresses[index]?.address;
        if (!addr) return;
        const row = e.currentTarget as HTMLElement;
        try {
            await navigator.clipboard.writeText(addr);
        } catch {
            return;
        }
        if (this.copiedIndex !== null && this.copiedIndex !== index) {
            this.morphIcon(this.copiedIndex, "copy", true);
        }
        this.clearTimers();
        this.copiedIndex = index;
        this.morphIcon(index, "check", false);
        if (!reducedMotion()) {
            gsap.fromTo(
                row,
                { scale: 1 },
                { scale: 1.018, duration: BOUNCE_S / 2, ease: "back.out(2)", yoyo: true, repeat: 1 },
            );
        }
        this.morphBackTimer = window.setTimeout(() => {
            this.morphBackTimer = 0;
            this.morphIcon(index, "copy", false);
        }, MORPH_BACK_DELAY_MS);
        this.resetTimer = window.setTimeout(() => {
            this.resetTimer = 0;
            if (this.copiedIndex === index) this.copiedIndex = null;
        }, BREATH_MS);
    }

    private onKeyDown(index: number, e: KeyboardEvent) {
        if (e.key !== "Enter" && e.key !== " ") return;
        e.preventDefault();
        this.onActivate(index, e);
    }

    private renderRow(item: JoinAddress, index: number) {
        const copied = this.copiedIndex === index;
        return html`
            <div
                class=${"row" + (copied ? " copied" : "")}
                data-index=${index}
                role="button"
                tabindex="0"
                aria-label=${`Copy ${item.label} address ${item.address}`}
                @click=${(e: Event) => this.onActivate(index, e)}
                @keydown=${(e: KeyboardEvent) => this.onKeyDown(index, e)}
            >
                <span class="label">
                    <decoder-v2
                        .text=${item.label}
                        .splashRounds=${[3, 5]}
                        splash-radius="1"
                        splash-tick-ms="22"
                        idle-min-ms="6000"
                        idle-max-ms="14000"
                        idle-radius="1"
                    ></decoder-v2>
                </span>
                <span class="address">
                    <decoder-v2
                        .text=${item.address}
                        .splashRounds=${[3, 5]}
                        splash-radius="1"
                        splash-tick-ms="22"
                        idle-min-ms="6000"
                        idle-max-ms="14000"
                        idle-radius="1"
                    ></decoder-v2>
                </span>
                <span class="icon" aria-hidden="true">
                    <svg viewBox="0 0 24 24" width="14" height="14"
                         stroke="currentColor" stroke-width="2"
                         stroke-linecap="round" stroke-linejoin="round" fill="none">
                        <path class="icon-path" d=${D_COPY}></path>
                    </svg>
                </span>
            </div>
        `;
    }

    render() {
        if (!this.addresses.length) return html``;
        return html`
            <div class="list">
                ${this.addresses.map((item, i) => this.renderRow(item, i))}
            </div>
        `;
    }

    static styles = css`
        :host {
            display: block;
            width: 100%;
            max-width: 380px;
            color: rgba(232, 240, 255, 0.85);
            font-size: 12px;
            line-height: 16px;
            font-variant-numeric: tabular-nums;
        }
        .list {
            display: flex;
            flex-direction: column;
            gap: 2px;
        }
        .row {
            display: grid;
            grid-template-columns: minmax(60px, auto) 1fr 14px;
            align-items: center;
            gap: 10px;
            padding: 5px 10px;
            min-height: 28px;
            border-radius: 7px;
            background: rgba(255, 255, 255, 0.035);
            border: 1px solid rgba(255, 255, 255, 0.05);
            cursor: pointer;
            outline: none;
            user-select: none;
            transition: background var(--motion-fast, 120ms ease),
                        border-color var(--motion-base, 220ms ease),
                        box-shadow var(--motion-base, 220ms ease);
            will-change: transform;
        }
        .row:hover {
            background: rgba(255, 255, 255, 0.08);
            border-color: rgba(255, 255, 255, 0.1);
        }
        .row:focus-visible {
            box-shadow: 0 0 0 2px color-mix(in srgb, var(--state-run) 60%, transparent);
        }
        .row:active {
            background: rgba(255, 255, 255, 0.1);
        }
        .row.copied {
            border-color: color-mix(in srgb, var(--state-run) 60%, transparent);
            background: color-mix(in srgb, var(--state-run) 10%, rgba(255, 255, 255, 0.04));
            box-shadow:
                0 0 0 1px color-mix(in srgb, var(--state-run) 35%, transparent),
                0 0 24px -4px color-mix(in srgb, var(--state-run) 55%, transparent);
            animation: breath ${BREATH_MS}ms cubic-bezier(.2, .0, .2, 1) 1;
        }
        @keyframes breath {
            0%   { box-shadow: 0 0 0 0 color-mix(in srgb, var(--state-run) 0%, transparent),
                              0 0 0 0 color-mix(in srgb, var(--state-run) 0%, transparent); }
            45%  { box-shadow: 0 0 0 2px color-mix(in srgb, var(--state-run) 55%, transparent),
                              0 0 32px -2px color-mix(in srgb, var(--state-run) 70%, transparent); }
            100% { box-shadow: 0 0 0 1px color-mix(in srgb, var(--state-run) 25%, transparent),
                              0 0 16px -4px color-mix(in srgb, var(--state-run) 30%, transparent); }
        }
        .label {
            color: rgba(232, 240, 255, 0.65);
            font-weight: 500;
            letter-spacing: 0.02em;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }
        .address {
            font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
            color: rgba(232, 240, 255, 0.95);
            letter-spacing: 0.01em;
            transition: color var(--motion-base, 220ms ease),
                        text-shadow var(--motion-base, 220ms ease);
        }
        .row.copied .address {
            color: var(--state-run);
            text-shadow: 0 0 12px color-mix(in srgb, var(--state-run) 45%, transparent);
        }
        .icon {
            display: inline-flex;
            align-items: center;
            justify-content: center;
            color: rgba(232, 240, 255, 0.55);
            transition: color var(--motion-base, 220ms ease);
        }
        .row.copied .icon {
            color: var(--state-run);
        }
        @media (prefers-reduced-motion: reduce) {
            .row { transition: none; }
            .row.copied { animation: none; }
        }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        "run-addresses": RunAddresses;
    }
}
