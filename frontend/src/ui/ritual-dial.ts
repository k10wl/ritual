import { LitElement, css, html, PropertyValues } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { gsap } from "gsap";
import { MorphSVGPlugin } from "gsap/MorphSVGPlugin";
import { Play, Square, X as XIcon, Download, Upload } from "lucide";
import svgpath from "svgpath";
import "./decoder-v2";
import "./stable-num";

gsap.registerPlugin(MorphSVGPlugin);

export type DialState = "idle" | "prep" | "run" | "final" | "fail";
export type DialGlyph = "play" | "stop" | "x" | "download" | "upload" | null;

const RADIUS = 100;
const CIRC = 2 * Math.PI * RADIUS;
const HOLD_S = 0.6;
const MORPH_S = 0.22;
const RESIZE_S = 0.28;
const ZOOM_S = 0.3;
const ZOOM_IDLE = 1.35;
const ZOOM_ACTIVE = 1.0;

type LucideChild = readonly [string, Record<string, string>];
type LucideIcon = ReadonlyArray<LucideChild>;

const shapeToD = ([tag, a]: LucideChild): string => {
    if (tag === "path") return a.d;
    if (tag === "line") return `M${a.x1} ${a.y1}L${a.x2} ${a.y2}`;
    if (tag === "circle") {
        const cx = +a.cx, cy = +a.cy, r = +a.r;
        return `M${cx - r} ${cy}A${r} ${r} 0 1 0 ${cx + r} ${cy}A${r} ${r} 0 1 0 ${cx - r} ${cy}Z`;
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

const GLYPHS: Record<Exclude<DialGlyph, null>, string> = {
    play:     compoundD(Play as LucideIcon),
    stop:     compoundD(Square as LucideIcon),
    x:        compoundD(XIcon as LucideIcon),
    download: compoundD(Download as LucideIcon),
    upload:   compoundD(Upload as LucideIcon),
};
const dFor = (g: DialGlyph): string => (g ? GLYPHS[g] : "");

const reducedMotion = (): boolean =>
    window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;

@customElement("ritual-dial")
export class RitualDial extends LitElement {
    @property({ reflect: true }) state: DialState = "idle";
    @property({ type: Number }) arc = 0;
    @property() label = "";
    @property() sub = "";
    @property() glyph: DialGlyph = null;
    @property({ type: Boolean, reflect: true }) disabled = false;

    @state() private holdProgress = 0;
    private pointerId: number | null = null;
    private holdTween?: gsap.core.Tween;
    private labelEl?: HTMLElement;
    private subEl?: HTMLElement;
    private clusterEl?: HTMLElement;
    private ringsEl?: SVGElement;
    private prevLabelH = 0;
    private prevSubH = 0;

    private get interactive() {
        return !this.disabled && (this.state === "idle" || this.state === "run" || this.state === "fail");
    }

    private get holdMode() {
        return this.state === "run";
    }

    private get effectiveArc() {
        return this.state === "run"
            ? 1 - this.holdProgress
            : Math.max(0, Math.min(1, this.arc));
    }

    private get dashOffset() {
        return CIRC * (1 - this.effectiveArc);
    }

    private get a11yLabel() {
        return this.sub ? `${this.label} — ${this.sub}` : this.label;
    }

    private get tabindex() {
        return this.interactive ? "0" : "-1";
    }

    private get pressed() {
        return this.holdProgress > 0 ? "true" : "false";
    }

    private onPointerDown = (e: PointerEvent) => {
        if (!this.interactive) return;
        this.pointerId = e.pointerId;
        (e.currentTarget as Element).setPointerCapture(e.pointerId);
        if (this.holdMode) this.startHold();
    };

    private onPointerMove = (e: PointerEvent) => {
        if (!this.interactive) return;
        const el = e.currentTarget as HTMLElement;
        const rect = el.getBoundingClientRect();
        const mx = ((e.clientX - rect.left) / rect.width) * 100;
        const my = ((e.clientY - rect.top) / rect.height) * 100;
        this.style.setProperty("--mx", `${mx}%`);
        this.style.setProperty("--my", `${my}%`);
    };

    private onPointerUp = (e: PointerEvent) => {
        if (this.pointerId !== e.pointerId) return;
        this.pointerId = null;
        if (this.holdMode) {
            this.endHold();
        } else if (this.interactive) {
            this.dispatchEvent(new CustomEvent("tap", { bubbles: true, composed: true }));
        }
    };

    private onPointerCancel = () => {
        this.pointerId = null;
        if (this.holdMode) this.endHold();
    };

    private onKeyDown = (e: KeyboardEvent) => {
        if (!this.interactive) return;
        if (e.key !== " " && e.key !== "Enter") return;
        e.preventDefault();
        if (this.holdMode && !this.holdActive) this.startHold();
    };

    private onKeyUp = (e: KeyboardEvent) => {
        if (!this.interactive) return;
        if (e.key !== " " && e.key !== "Enter") return;
        e.preventDefault();
        if (this.holdMode) {
            this.endHold();
        } else {
            this.dispatchEvent(new CustomEvent("tap", { bubbles: true, composed: true }));
        }
    };

    private get holdActive() {
        return !!this.holdTween && this.holdTween.isActive();
    }

    private ensureHoldTween() {
        if (this.holdTween) return;
        this.holdTween = gsap.to(this, {
            holdProgress: 1,
            duration: HOLD_S,
            ease: "none",
            paused: true,
            onUpdate: () => this.requestUpdate(),
            onComplete: () =>
                this.dispatchEvent(new CustomEvent("hold-commit", { bubbles: true, composed: true })),
        });
    }

    private startHold() {
        this.ensureHoldTween();
        this.holdTween!.play();
    }

    private endHold() {
        if (!this.holdTween) return;
        if (this.holdProgress <= 0) return;
        this.holdTween.reverse();
    }

    private resetHold() {
        this.holdTween?.kill();
        this.holdTween = undefined;
        this.holdProgress = 0;
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        this.resetHold();
    }

    willUpdate(changed: PropertyValues) {
        if (changed.has("label") && this.labelEl) this.prevLabelH = this.labelEl.offsetHeight;
        if (changed.has("sub") && this.subEl) this.prevSubH = this.subEl.offsetHeight;
        if (changed.has("state") && this.state !== "run" && changed.get("state") === "run") {
            this.resetHold();
        }
    }

    private animateHeight(el: HTMLElement | undefined, prev: number) {
        if (!el) return;
        if (prev <= 0) return;
        const next = el.offsetHeight;
        if (next === prev) return;
        if (reducedMotion()) return;
        gsap.killTweensOf(el);
        gsap.fromTo(
            el,
            { height: prev },
            {
                height: next,
                duration: RESIZE_S,
                ease: "power2.inOut",
                onComplete: () => { el.style.height = ""; },
            },
        );
    }

    firstUpdated() {
        const root = this.shadowRoot;
        this.labelEl = root?.querySelector(".label") as HTMLElement;
        this.subEl = root?.querySelector(".sub") as HTMLElement;
        this.clusterEl = root?.querySelector(".cluster") as HTMLElement;
        this.ringsEl = root?.querySelector(".rings") as SVGElement;
        this.applyZoom(false);
        const el = this.glyphPathEl();
        if (!el) return;
        const d = dFor(this.glyph);
        if (!d) return;
        el.setAttribute("d", d);
    }

    updated(changed: PropertyValues) {
        if (changed.has("label")) this.animateHeight(this.labelEl, this.prevLabelH);
        if (changed.has("sub")) this.animateHeight(this.subEl, this.prevSubH);
        if (changed.has("state")) this.applyZoom(true);
        if (!changed.has("glyph")) return;
        const old = changed.get("glyph") as DialGlyph | undefined;
        if (old === undefined) return;
        this.morphTo(this.glyph);
    }

    private applyZoom(animate: boolean) {
        if (!this.clusterEl || !this.ringsEl) return;
        const scale = this.state === "idle" ? ZOOM_IDLE : ZOOM_ACTIVE;
        const opacity = this.state === "idle" ? 0 : 1;
        if (!animate || reducedMotion()) {
            gsap.set(this.clusterEl, { scale, transformOrigin: "50% 42%" });
            gsap.set(this.ringsEl, { opacity });
            return;
        }
        gsap.to(this.clusterEl, {
            scale,
            duration: ZOOM_S,
            ease: scale === ZOOM_ACTIVE ? "power3.out" : "power2.inOut",
            transformOrigin: "50% 42%",
            overwrite: "auto",
        });
        gsap.to(this.ringsEl, {
            opacity,
            duration: ZOOM_S,
            ease: "power2.inOut",
            overwrite: "auto",
        });
    }

    private glyphPathEl(): SVGPathElement | null {
        return this.shadowRoot?.querySelector(".glyph-path") as SVGPathElement | null ?? null;
    }

    private morphTo(to: DialGlyph) {
        const el = this.glyphPathEl();
        if (!el) return;
        const targetD = dFor(to);
        if (!targetD) {
            el.setAttribute("d", "");
            return;
        }
        if (reducedMotion()) {
            el.setAttribute("d", targetD);
            return;
        }
        gsap.to(el, {
            duration: MORPH_S,
            ease: "power2.inOut",
            morphSVG: targetD,
            overwrite: true,
        });
    }

    private renderSub() {
        if (/\d/.test(this.sub)) return this.sub;
        const isPlaceholder = !/[a-zA-Z]/.test(this.sub);
        const idleMin = isPlaceholder ? 50 : 1400;
        const idleMax = isPlaceholder ? 120 : 2800;
        const idleRadius = isPlaceholder ? Math.max(1, this.sub.length) : 1;
        return html`<decoder-v2
            .text=${this.sub}
            .splashRounds=${[3, 5]}
            splash-radius="1"
            splash-tick-ms="22"
            idle-min-ms=${idleMin}
            idle-max-ms=${idleMax}
            idle-radius=${idleRadius}
        ></decoder-v2>`;
    }

    render() {
        return html`
            <div class="dial" data-state=${this.state}>
                <svg class="rings" viewBox="-120 -120 240 240" aria-hidden="true">
                    <circle class="track" r=${RADIUS} />
                    <circle class="arc" r=${RADIUS}
                            stroke-dasharray=${CIRC}
                            stroke-dashoffset=${this.dashOffset} />
                </svg>
                <button class="hit"
                        ?disabled=${!this.interactive}
                        tabindex=${this.tabindex}
                        aria-label=${this.a11yLabel}
                        aria-pressed=${this.pressed}
                        @pointerdown=${this.onPointerDown}
                        @pointermove=${this.onPointerMove}
                        @pointerup=${this.onPointerUp}
                        @pointercancel=${this.onPointerCancel}
                        @keydown=${this.onKeyDown}
                        @keyup=${this.onKeyUp}>
                    <div class="cluster">
                        <div class="glyph-slot">
                            <svg class="glyph"
                                 viewBox="0 0 24 24"
                                 stroke="currentColor"
                                 stroke-width="2"
                                 stroke-linecap="round"
                                 stroke-linejoin="round"
                                 fill="none"
                                 aria-hidden="true">
                                <path class="glyph-path" />
                                <path class="glyph-path" />
                                <path class="glyph-path" />
                            </svg>
                        </div>
                        <div class="label">
                            <decoder-v2 .text=${this.label}></decoder-v2>
                        </div>
                        <div class="sub" ?data-empty=${!this.sub}>
                            <stable-num chars="6" align="center">
                                ${this.renderSub()}
                            </stable-num>
                        </div>
                    </div>
                </button>
            </div>
        `;
    }

    static styles = css`
        @property --c {
            syntax: "<color>";
            inherits: true;
            initial-value: #2563eb;
        }
        :host {
            display: flex;
            align-items: center;
            justify-content: center;
            --c: var(--state-idle);
            --radiance:    color-mix(in srgb, var(--c) 55%, transparent);
            --radiance-hi: color-mix(in srgb, var(--c) 75%, transparent);
            --radiance-lo: color-mix(in srgb, var(--c) 22%, transparent);
            transition: --c var(--motion-base, 220ms ease);
        }
        :host([state="idle"])  { --c: var(--state-idle); }
        :host([state="prep"])  { --c: var(--state-prep); }
        :host([state="run"])   { --c: var(--state-run); }
        :host([state="final"]) { --c: var(--state-final); }
        :host([state="fail"])  { --c: var(--state-fail); }

        .dial {
            position: relative;
            width: 280px;
            height: 280px;
            border-radius: 50%;
            background:
                radial-gradient(circle at 50% 0%, var(--stone-edge), transparent 55%),
                radial-gradient(circle at 50% 100%,
                    color-mix(in srgb, var(--c) 18%, transparent),
                    transparent 65%),
                linear-gradient(180deg, var(--stone-base) 0%, var(--stone-dark) 100%);
            box-shadow:
                inset 0 1px 0 var(--stone-edge),
                inset 0 -1px 2px var(--stone-groove),
                0 28px 60px -18px var(--radiance),
                0 0 0 1px var(--stone-bevel);
            transition: box-shadow var(--motion-base, 220ms ease),
                        background var(--motion-base, 220ms ease),
                        transform var(--motion-fast, 120ms ease);
        }
        .dial::after {
            content: "";
            position: absolute;
            inset: 0;
            border-radius: 50%;
            background: radial-gradient(circle 140px at var(--mx, 50%) var(--my, 50%),
                var(--radiance-lo),
                transparent 62%);
            opacity: 0;
            transition: opacity var(--motion-fast, 120ms ease);
            pointer-events: none;
            mix-blend-mode: screen;
        }
        .dial:has(.hit:hover:not(:disabled))::after {
            opacity: 1;
        }
        .dial:has(.hit:hover:not(:disabled)) {
            transform: translateY(-1px);
        }
        :host(:not([state="run"])) .dial:has(.hit:hover:not(:disabled)) {
            box-shadow:
                inset 0 1px 0 var(--stone-edge),
                inset 0 -1px 2px var(--stone-groove),
                0 36px 80px -16px var(--radiance-hi),
                0 0 0 1px color-mix(in srgb, var(--c) 18%, var(--stone-bevel));
        }
        .dial:has(.hit:active:not(:disabled)) {
            transform: translateY(0) scale(0.985);
        }
        :host([state="run"]) .dial {
            animation: breathe 2.6s ease-in-out infinite alternate;
        }
        @keyframes breathe {
            from { box-shadow:
                inset 0 1px 0 var(--stone-edge),
                inset 0 -1px 2px var(--stone-groove),
                0 24px 50px -20px var(--radiance),
                0 0 0 1px var(--stone-bevel); }
            to   { box-shadow:
                inset 0 1px 0 var(--stone-edge),
                inset 0 -1px 2px var(--stone-groove),
                0 36px 80px -16px var(--radiance-hi),
                0 0 0 1px color-mix(in srgb, var(--c) 25%, var(--stone-bevel)); }
        }

        .rings {
            position: absolute;
            inset: 0;
            width: 100%;
            height: 100%;
            pointer-events: none;
        }
        .track {
            fill: none;
            stroke: var(--dial-track);
            stroke-width: 8;
        }
        .arc {
            fill: none;
            stroke: var(--c);
            stroke-width: 8;
            stroke-linecap: round;
            filter: drop-shadow(0 0 8px var(--radiance-hi));
            transform: rotate(-90deg);
            transform-box: fill-box;
            transform-origin: center;
            transition: stroke var(--motion-base, 220ms ease);
        }
        :host([state="run"]) .arc {
            transition: stroke var(--motion-base, 220ms ease);
        }
        .hit {
            position: absolute;
            inset: 0;
            border-radius: 50%;
            border: none;
            background: transparent;
            color: inherit;
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            cursor: pointer;
            outline: none;
            padding: 0 var(--space-6);
        }
        .hit:disabled { cursor: default; }
        .hit:focus-visible {
            box-shadow: 0 0 0 3px color-mix(in srgb, var(--c) 50%, transparent);
        }
        .cluster {
            display: flex;
            flex-direction: column;
            align-items: center;
            will-change: transform;
        }
        .glyph-slot {
            position: relative;
            height: 64px;
            width: 64px;
            display: flex;
            align-items: center;
            justify-content: center;
            margin-bottom: 10px;
        }
        .glyph {
            width: 56px;
            height: 56px;
            color: var(--c);
            filter: drop-shadow(0 4px 12px var(--radiance));
            transition: color var(--motion-base, 220ms ease),
                        filter var(--motion-base, 220ms ease);
        }
        .label {
            font-size: var(--fs-4);
            line-height: 24px;
            color: var(--text-strong);
            text-align: center;
            overflow: hidden;
        }
        .sub {
            font-size: var(--fs-2);
            line-height: 18px;
            color: var(--text-muted);
            letter-spacing: 0.02em;
            margin-top: var(--space-1);
            text-align: center;
            overflow: hidden;
            font-variant-numeric: tabular-nums;
        }
        .sub[data-empty] {
            margin-top: 0;
            line-height: 0;
            height: 0;
        }
        @media (prefers-reduced-motion: reduce) {
            .arc, .glyph, .dial { transition: none; animation: none; }
        }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        "ritual-dial": RitualDial;
    }
}
