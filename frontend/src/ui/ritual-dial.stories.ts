import { LitElement, html } from "lit";
import { customElement, state } from "lit/decorators.js";
import { gsap } from "gsap";
import "./ritual-dial";
import type { DialGlyph, DialState } from "./ritual-dial";

const TOTAL_MB = 953;
const TRANSFER_S = 3;
const HOLD_S = 1.6;

@customElement("dial-cycle-demo")
export class DialCycleDemo extends LitElement {
    @state() private state: DialState = "idle";
    @state() private arc = 0;
    @state() private glyph: DialGlyph = "play";
    @state() private label = "Start";
    @state() private sub = "";

    private tl?: gsap.core.Timeline;

    connectedCallback() {
        super.connectedCallback();
        const a = { v: 0 };
        const writeProgress = () => {
            this.arc = a.v;
            this.sub = `${Math.round(a.v * TOTAL_MB)} of ${TOTAL_MB} MB`;
        };
        const tl = gsap.timeline({ repeat: -1 });
        tl.call(() => {
            this.state = "idle"; this.arc = 0; this.glyph = "play";
            this.label = "Start"; this.sub = "";
        });
        tl.to({}, { duration: HOLD_S });
        tl.call(() => {
            this.state = "prep"; this.glyph = "download"; this.label = "Getting ready";
            a.v = 0; writeProgress();
        });
        tl.to(a, { v: 1, duration: TRANSFER_S, ease: "power2.out", onUpdate: writeProgress });
        tl.call(() => {
            this.state = "run"; this.arc = 1; this.glyph = "stop";
            this.label = "Ready to play"; this.sub = "Hold to stop";
        });
        tl.to({}, { duration: HOLD_S });
        tl.call(() => {
            this.state = "final"; this.glyph = "upload"; this.label = "Saving";
            a.v = 0; writeProgress();
        });
        tl.to(a, { v: 1, duration: TRANSFER_S, ease: "power2.out", onUpdate: writeProgress });
        tl.call(() => {
            this.state = "fail"; this.arc = 0.42; this.glyph = "x";
            this.label = "Couldn't finish"; this.sub = "Tap to try again";
        });
        tl.to({}, { duration: HOLD_S });
        this.tl = tl;
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        this.tl?.kill();
    }

    render() {
        return html`
            <ritual-dial
                .state=${this.state}
                .arc=${this.arc}
                .glyph=${this.glyph}
                .label=${this.label}
                .sub=${this.sub}
            ></ritual-dial>
        `;
    }
}

interface Args {
    state: DialState;
    arc: number;
    label: string;
    sub: string;
    glyph: DialGlyph | "";
    disabled: boolean;
}

export default {
    title: "Dial / RitualDial",
    component: "ritual-dial",
    argTypes: {
        state: {
            control: { type: "select" },
            options: ["idle", "prep", "run", "final", "fail"],
        },
        arc: { control: { type: "range", min: 0, max: 1, step: 0.01 } },
        label: { control: { type: "text" } },
        sub: { control: { type: "text" } },
        glyph: {
            control: { type: "select" },
            options: ["", "play", "stop", "x", "download", "upload"],
        },
        disabled: { control: { type: "boolean" } },
    },
    args: {
        state: "idle",
        arc: 0,
        label: "Start",
        sub: "",
        glyph: "play",
        disabled: false,
    },
};

export const Playground = (a: Args) => html`
    <ritual-dial
        .state=${a.state}
        .arc=${a.arc}
        .label=${a.label}
        .sub=${a.sub}
        .glyph=${a.glyph === "" ? null : (a.glyph as DialGlyph)}
        ?disabled=${a.disabled}
    ></ritual-dial>
`;

export const Cycle = () => html`<dial-cycle-demo></dial-cycle-demo>`;
