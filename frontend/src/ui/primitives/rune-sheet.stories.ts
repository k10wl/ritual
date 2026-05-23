import { html } from "lit";
import "./rune-sheet";
import "./rune-button";
import "./rune-field";
import "./rune-disclosure";
import type { RuneSheet } from "./rune-sheet";

export default {
    title: "Primitives / Rune Sheet",
    component: "rune-sheet",
    parameters: {
        docs: {
            description: {
                component:
                    "Modal sheet over native <dialog>. Browser supplies focus trap + Escape dismiss; " +
                    "backdrop-click dismiss added. Emits open / close / dismiss with reason. " +
                    "HIG: https://developer.apple.com/design/human-interface-guidelines/sheets",
            },
        },
    },
};

const openSheet = (id: string) => () => {
    const sheet = document.getElementById(id) as RuneSheet | null;
    sheet?.show();
};

const closeSheet = (id: string) => () => {
    const sheet = document.getElementById(id) as RuneSheet | null;
    sheet?.close();
};

export const Basic = () => html`
    <div style="padding:var(--space-4);">
        <rune-button variant="primary" @press=${openSheet("sheet-basic")}>Open sheet</rune-button>

        <rune-sheet id="sheet-basic" heading="Welcome">
            <p style="margin:0; color:var(--text-muted);">
                Native &lt;dialog&gt; provides focus trap and Escape-to-close.
                Click outside the sheet to dismiss via backdrop.
            </p>
            <rune-button slot="footer" variant="tinted" @press=${closeSheet("sheet-basic")}>Close</rune-button>
        </rune-sheet>
    </div>
`;

export const AdvancedSettings = () => html`
    <div style="padding:var(--space-4);">
        <rune-button variant="primary" @press=${openSheet("sheet-014")}>Open settings (014 preview)</rune-button>

        <rune-sheet id="sheet-014" heading="Advanced">
            <div style="display:flex; flex-direction:column; gap:var(--space-4);">
                <rune-field
                    type="number"
                    label="Port"
                    value="25565"
                    min="1024"
                    max="65535"
                    hint="Range 1024–65535."
                ></rune-field>
                <rune-field
                    type="number"
                    label="Memory (MB)"
                    value="2048"
                    min="512"
                    step="256"
                    hint="At least 1024 MB recommended."
                ></rune-field>
                <rune-disclosure>
                    <span slot="summary">Diagnostics</span>
                    <p style="margin:0; color:var(--text-muted); font-size:var(--fs-caption);">
                        Optional telemetry settings live here.
                    </p>
                </rune-disclosure>
            </div>
            <rune-button slot="footer" variant="tinted" @press=${closeSheet("sheet-014")}>Cancel</rune-button>
            <rune-button slot="footer" variant="primary" @press=${closeSheet("sheet-014")}>Save</rune-button>
        </rune-sheet>
    </div>
`;
