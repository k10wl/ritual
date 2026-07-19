import { html } from "lit";
import "./run-console-link";

export default {
    title: "Components / Run Console Link",
    component: "run-console-link",
};

// The lone RUN-stage affordance that opens the server console (design-log/043).
export const Playground = () => html`
    <run-console-link
        @press=${() => console.log("press → ShowLogs")}
    ></run-console-link>
`;

// As it sits under the address rows in the playing under-slot.
export const UnderAddresses = () => html`
    <div style="display:flex;flex-direction:column;align-items:center;gap:12px;max-width:380px;">
        <div style="opacity:.5;font:12px var(--font-mono);color:var(--text-muted);">…address rows above…</div>
        <run-console-link @press=${() => console.log("press → ShowLogs")}></run-console-link>
    </div>
`;
