import { html } from "lit";
import "./rune-disclosure";
import "./rune-button";

export default {
    title: "Primitives / Rune Disclosure",
    component: "rune-disclosure",
    parameters: {
        docs: {
            description: {
                component:
                    "Native <details>/<summary> wrapper. Animates height via `interpolate-size: allow-keywords`. " +
                    "Emits `open` / `close`. " +
                    "HIG: https://developer.apple.com/design/human-interface-guidelines/disclosure-controls",
            },
        },
    },
};

export const Closed = () => html`
    <div style="padding:var(--space-4); max-width:380px;">
        <rune-disclosure>
            <span slot="summary">Advanced</span>
            <p style="margin:0; color:var(--text-muted);">Hidden by default.</p>
        </rune-disclosure>
    </div>
`;

export const Open = () => html`
    <div style="padding:var(--space-4); max-width:380px;">
        <rune-disclosure open>
            <span slot="summary">Advanced</span>
            <div style="display:flex; flex-direction:column; gap:var(--space-2);">
                <p style="margin:0; color:var(--text-muted);">Body content revealed.</p>
                <rune-button variant="tinted" size="sm">Action</rune-button>
            </div>
        </rune-disclosure>
    </div>
`;

const onChange = (e: Event) => {
    // eslint-disable-next-line no-console
    console.log(e.type);
};

export const Listening = () => html`
    <div style="padding:var(--space-4); max-width:380px;"
         @open=${onChange}
         @close=${onChange}>
        <rune-disclosure>
            <span slot="summary">Listen — open/close logs to console</span>
            <p style="margin:0; color:var(--text-muted);">Toggle me.</p>
        </rune-disclosure>
    </div>
`;
