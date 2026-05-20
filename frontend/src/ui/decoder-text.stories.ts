import { LitElement, html } from "lit";
import { customElement, state } from "lit/decorators.js";
import "./decoder-text";

const PHRASES = [
    "INITIALIZING",
    "Linking uplink…",
    "Authenticated",
    "Ready to play",
    "Couldn't finish getting ready",
    "476 MB of 953 MB",
    "Tap to try again",
];

@customElement("decoder-cycle-demo")
export class DecoderCycleDemo extends LitElement {
    @state() private i = 0;
    private timer = 0;

    connectedCallback() {
        super.connectedCallback();
        this.timer = window.setInterval(() => {
            this.i = (this.i + 1) % PHRASES.length;
        }, 2000);
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        clearInterval(this.timer);
    }

    render() {
        return html`
            <decoder-text
                style="font-size: 22px; font-weight: 600; color: #e5e7eb;"
                .text=${PHRASES[this.i]}
                duration="0.9"
            ></decoder-text>
        `;
    }
}

interface Args {
    text: string;
    duration: number;
}

export default {
    title: "Text / DecoderText",
    component: "decoder-text",
    argTypes: {
        text: { control: { type: "text" } },
        duration: { control: { type: "range", min: 0.1, max: 3, step: 0.05 } },
    },
    args: {
        text: "Ready to play",
        duration: 0.8,
    },
};

export const Playground = (a: Args) => html`
    <decoder-text
        style="font-size: 22px; font-weight: 600; color: #e5e7eb;"
        .text=${a.text}
        .duration=${a.duration}
    ></decoder-text>
`;

export const Cycle = () => html`<decoder-cycle-demo></decoder-cycle-demo>`;
