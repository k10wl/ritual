import { LitElement, html } from "lit";
import { customElement, state } from "lit/decorators.js";
import "./decoder";

const PHRASES = [
    "INITIALIZING",
    "Linking uplink…",
    "Authenticated",
    "Ready to play",
    "Couldn't finish getting ready",
    "476 MB of 953 MB",
    "Tap to try again",
];

@customElement("rune-decoder-cycle")
export class RuneDecoderCycle extends LitElement {
    @state() private i = 0;
    private timer = 0;

    connectedCallback() {
        super.connectedCallback();
        this.timer = window.setInterval(() => {
            this.i = (this.i + 1) % PHRASES.length;
        }, 2500);
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        clearInterval(this.timer);
    }

    render() {
        return html`
            <rune-decoder
                style="font-size: 22px; font-weight: 600; color: #e5e7eb;"
                .text=${PHRASES[this.i]}
            ></rune-decoder>
        `;
    }
}

interface Args {
    text: string;
    splashCount: number;
    splashRadius: number;
    splashTickMs: number;
    idleMinMs: number;
    idleMaxMs: number;
    idleRadius: number;
    idleTickMs: number;
    seed: number | null;
}

export default {
    title: "Primitives / Rune Decoder",
    component: "rune-decoder",
    argTypes: {
        text: { control: { type: "text" } },
        splashCount: { control: { type: "range", min: 1, max: 5, step: 1 } },
        splashRadius: { control: { type: "range", min: 1, max: 10, step: 1 } },
        splashTickMs: { control: { type: "range", min: 10, max: 200, step: 5 } },
        idleMinMs: { control: { type: "range", min: 500, max: 10000, step: 100 } },
        idleMaxMs: { control: { type: "range", min: 500, max: 10000, step: 100 } },
        idleRadius: { control: { type: "range", min: 0, max: 5, step: 1 } },
        idleTickMs: { control: { type: "range", min: 10, max: 200, step: 5 } },
        seed: { control: { type: "number" } },
    },
    args: {
        text: "Ready to play",
        splashCount: 1,
        splashRadius: 3,
        splashTickMs: 50,
        idleMinMs: 2000,
        idleMaxMs: 5000,
        idleRadius: 1,
        idleTickMs: 80,
        seed: null,
    },
};

export const Playground = (a: Args) => html`
    <rune-decoder
        style="font-size: 22px; font-weight: 600; color: #e5e7eb;"
        .text=${a.text}
        .splashCount=${a.splashCount}
        .splashRadius=${a.splashRadius}
        .splashTickMs=${a.splashTickMs}
        .idleMinMs=${a.idleMinMs}
        .idleMaxMs=${a.idleMaxMs}
        .idleRadius=${a.idleRadius}
        .idleTickMs=${a.idleTickMs}
        .seed=${a.seed}
    ></rune-decoder>
`;

export const Cycle = () => html`<rune-decoder-cycle></rune-decoder-cycle>`;
