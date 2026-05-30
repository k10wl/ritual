import { html } from "lit";
import "./rune-stack";
import "./rune-row";
import "./rune-button";
import "./rune-field";
import type { NavController, NavView } from "../contexts/nav-context";

export default {
    title: "Primitives / Rune Stack",
    component: "rune-stack",
    parameters: {
        docs: {
            description: {
                component:
                    "Navigation stack (design-log/034). Selecting a row slides a full-screen pane in " +
                    "from the right; the ← bar slides it out. Arbitrary depth, lazy panes, no modals. " +
                    "HIG: https://developer.apple.com/design/human-interface-guidelines/navigation-and-search",
            },
        },
    },
};

// A fixed frame approximating the 560×720 app window (design-log/023) so the
// whole-screen translate reads honestly in isolation.
const frame = (inner: unknown) => html`
    <div style="width:420px; height:560px; border:1px solid var(--stone-bevel);
                border-radius:var(--radius-lg); overflow:hidden; background:var(--surface, #111);">
        ${inner}
    </div>
`;

const note = (text: string) => html`
    <p style="margin:0; padding:var(--space-4); color:var(--text-muted); line-height:1.6;">${text}</p>
`;

// ── A mock Sync view (031 tenant): explicit check → humane verdict. ──────────
const syncView: NavView = {
    id: "sync",
    title: "Sync",
    render: () => html`
        <div style="padding:var(--space-4); display:flex; flex-direction:column; gap:var(--space-3);">
            <rune-button variant="tinted" @press=${(e: Event) => {
                const host = (e.target as HTMLElement).parentElement!;
                host.querySelector(".verdict")!.textContent = "A newer world is waiting.";
                (host.querySelector(".act") as HTMLElement).hidden = false;
            }}>Check remote</rune-button>
            <p class="verdict" style="margin:0; color:var(--text-muted);">Not checked yet.</p>
            <rune-button class="act" variant="primary" hidden>⬇ Download</rune-button>
        </div>
    `,
};

// ── A mock Retention view (033 tenant): arbitrary content. ──────────────────
const retentionView: NavView = {
    id: "retention",
    title: "Retention",
    render: () => note("Borg-style tier picker lands here (design-log/033). Any content renders in a view."),
};

// ── Files view: nested selections that push deeper (req. 5/6). ──────────────
const filesView: NavView = {
    id: "files",
    title: "Files",
    render: (nav: NavController) => html`
        <div style="padding:var(--space-2);">
            <rune-row pressable @press=${() => nav.push(syncView)}>
                <span>Sync</span><span slot="trailing" style="color:var(--text-faint)">›</span>
            </rune-row>
            <rune-row pressable @press=${() => nav.push(retentionView)}>
                <span>Retention</span><span slot="trailing" style="color:var(--text-faint)">›</span>
            </rune-row>
        </div>
    `,
};

// Root → Files → {Sync, Retention}. The drill-down everyone walks.
export const DrillDown = () =>
    frame(html`
        <rune-stack>
            <div style="padding:var(--space-4); display:flex; flex-direction:column;
                        gap:var(--space-4); align-items:center; justify-content:center; height:100%;">
                <div style="font-size:var(--fs-title); color:var(--text-strong);">main stage</div>
                <rune-button variant="tinted" @press=${(e: Event) => {
                    const stack = (e.target as HTMLElement).closest("rune-stack") as any;
                    stack.push(filesView);
                }}>files →</rune-button>
            </div>
        </rune-stack>
    `);

// Deep nesting: each view pushes a deeper clone (req. 5). Unwind with ←.
const deep = (n: number): NavView => ({
    id: `level-${n}`,
    title: `Level ${n}`,
    render: (nav) => html`
        <div style="padding:var(--space-4); display:flex; flex-direction:column; gap:var(--space-3);">
            ${note(`Depth ${n}. Push goes deeper; ← pops back.`)}
            <rune-button variant="tinted" @press=${() => nav.push(deep(n + 1))}>deeper →</rune-button>
        </div>
    `,
});

export const DeepNesting = () =>
    frame(html`
        <rune-stack>
            <div style="padding:var(--space-4); height:100%; display:flex;
                        flex-direction:column; gap:var(--space-3); justify-content:center;">
                ${note("Push as deep as you like; the back bar unwinds one level at a time.")}
                <rune-button variant="tinted" @press=${(e: Event) => {
                    const stack = (e.target as HTMLElement).closest("rune-stack") as any;
                    stack.push(deep(1));
                }}>start →</rune-button>
            </div>
        </rune-stack>
    `);

// A view may render anything — a form, here.
export const ArbitraryContent = () =>
    frame(html`
        <rune-stack>
            <div style="padding:var(--space-4); height:100%; display:flex; align-items:center; justify-content:center;">
                <rune-button variant="tinted" @press=${(e: Event) => {
                    const stack = (e.target as HTMLElement).closest("rune-stack") as any;
                    stack.push({
                        id: "settings",
                        title: "Settings",
                        render: () => html`
                            <div style="padding:var(--space-4); display:flex; flex-direction:column; gap:var(--space-4);">
                                <rune-field type="number" label="Port" value="25565" hint="1–65535."></rune-field>
                                <rune-field type="number" label="Memory (GB)" value="4" hint="≥ 4 GB."></rune-field>
                            </div>
                        `,
                    });
                }}>settings →</rune-button>
            </div>
        </rune-stack>
    `);
