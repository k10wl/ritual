import { html } from "lit";

export default {
    title: "Tokens / Overview",
    parameters: {
        docs: {
            description: {
                component:
                    "Visual reference for the design-system tokens declared in `public/style.css`. " +
                    "Update tokens there; this page reflects them automatically. " +
                    "See `frontend/CLAUDE.md` for the rules.",
            },
        },
    },
};

const sectionStyle = "padding:var(--space-4); font-family:var(--font-mono); color:var(--text);";
const headingStyle = "font-size:var(--fs-title); margin:0 0 var(--space-3); color:var(--text-strong);";
const labelStyle  = "font-size:var(--fs-caption); color:var(--text-muted); letter-spacing:0.06em; text-transform:uppercase;";
const rowStyle    = "display:flex; align-items:center; gap:var(--space-3); padding:var(--space-2) 0;";

const swatch = (token: string, height = "32px") => html`
    <div style=${rowStyle}>
        <div style="width:96px; height:${height}; background:var(${token}); border-radius:var(--radius-sm); box-shadow:inset 0 1px 0 var(--stone-bevel);"></div>
        <code style="font-size:var(--fs-caption); color:var(--text);">${token}</code>
    </div>
`;

const typeRow = (token: string) => html`
    <div style=${rowStyle}>
        <span style="font-size:var(${token}); color:var(--text-strong);">Departure Mono Glyphs</span>
        <code style="font-size:var(--fs-caption); color:var(--text-muted);">${token}</code>
    </div>
`;

const spaceRow = (token: string) => html`
    <div style=${rowStyle}>
        <div style="width:var(${token}); height:14px; background:var(--rune-soft); border-radius:2px;"></div>
        <code style="font-size:var(--fs-caption); color:var(--text-muted);">${token}</code>
    </div>
`;

const motionRow = (token: string) => html`
    <div style=${rowStyle}>
        <div style="width:80px; height:12px; background:var(--stone-edge); border-radius:6px; position:relative; overflow:hidden;">
            <div style="position:absolute; inset:0; background:var(--rune); transform-origin:left; transform:scaleX(0); animation: tokens-ping var(${token}) ease infinite alternate;"></div>
        </div>
        <code style="font-size:var(--fs-caption); color:var(--text-muted);">${token}</code>
    </div>
    <style>@keyframes tokens-ping { to { transform: scaleX(1); } }</style>
`;

export const Type = () => html`
    <section style=${sectionStyle}>
        <h2 style=${headingStyle}>Type — semantic clamp scale</h2>
        ${typeRow("--fs-caption")}
        ${typeRow("--fs-body")}
        ${typeRow("--fs-title")}
        ${typeRow("--fs-display")}
    </section>
`;

export const Spacing = () => html`
    <section style=${sectionStyle}>
        <h2 style=${headingStyle}>Spacing — 4px grid</h2>
        ${spaceRow("--space-1")}
        ${spaceRow("--space-2")}
        ${spaceRow("--space-3")}
        ${spaceRow("--space-4")}
        ${spaceRow("--space-5")}
        ${spaceRow("--space-6")}
        ${spaceRow("--space-7")}
    </section>
`;

export const Surfaces = () => html`
    <section style=${sectionStyle}>
        <h2 style=${headingStyle}>Surfaces — stone elevations</h2>
        ${swatch("--surface-flat",     "48px")}
        ${swatch("--surface-recessed", "48px")}
        ${swatch("--surface-raised",   "48px")}
        ${swatch("--surface-floating", "48px")}
        ${swatch("--surface-overlay",  "48px")}
    </section>
`;

export const Stone = () => html`
    <section style=${sectionStyle}>
        <h2 style=${headingStyle}>Stone — substrate hues</h2>
        ${swatch("--stone-deep")}
        ${swatch("--stone-dark")}
        ${swatch("--stone-base")}
        ${swatch("--stone-bevel")}
        ${swatch("--stone-edge")}
        ${swatch("--stone-groove")}
    </section>
`;

export const Text = () => html`
    <section style=${sectionStyle}>
        <h2 style=${headingStyle}>Text — opacity tiers</h2>
        <div style=${rowStyle}><span style="color:var(--text-strong);">Strong</span><code style=${labelStyle}>--text-strong</code></div>
        <div style=${rowStyle}><span style="color:var(--text);">Default</span><code style=${labelStyle}>--text</code></div>
        <div style=${rowStyle}><span style="color:var(--text-muted);">Muted</span><code style=${labelStyle}>--text-muted</code></div>
        <div style=${rowStyle}><span style="color:var(--text-faint);">Faint</span><code style=${labelStyle}>--text-faint</code></div>
    </section>
`;

export const Rune = () => html`
    <section style=${sectionStyle}>
        <h2 style=${headingStyle}>Rune — cyan-teal liveness</h2>
        ${swatch("--rune")}
        ${swatch("--rune-hi")}
        ${swatch("--rune-soft")}
    </section>
`;

export const Phases = () => html`
    <section style=${sectionStyle}>
        <h2 style=${headingStyle}>Phase palette — sync state machine</h2>
        ${swatch("--state-idle")}
        ${swatch("--state-prep")}
        ${swatch("--state-run")}
        ${swatch("--state-final")}
        ${swatch("--state-fail")}
    </section>
`;

export const Feedback = () => html`
    <section style=${sectionStyle}>
        <h2 style=${headingStyle}>Feedback — pressable</h2>
        ${swatch("--feedback-hover")}
        ${swatch("--feedback-pressed")}
        ${swatch("--feedback-loading")}
        <div style=${rowStyle}>
            <div style="width:96px; height:32px; background:var(--stone-edge); border-radius:var(--radius-sm); opacity:var(--feedback-disabled);"></div>
            <code style=${labelStyle}>--feedback-disabled (opacity)</code>
        </div>
    </section>
`;

export const Motion = () => html`
    <section style=${sectionStyle}>
        <h2 style=${headingStyle}>Motion — durations</h2>
        ${motionRow("--motion-fast")}
        ${motionRow("--motion-base")}
        ${motionRow("--motion-press")}
        ${motionRow("--motion-reveal")}
        ${motionRow("--motion-settle")}
    </section>
`;

export const Radius = () => html`
    <section style=${sectionStyle}>
        <h2 style=${headingStyle}>Radius</h2>
        <div style=${rowStyle}>
            <div style="width:96px; height:48px; background:var(--stone-edge); border-radius:var(--radius-sm);"></div>
            <code style=${labelStyle}>--radius-sm</code>
        </div>
        <div style=${rowStyle}>
            <div style="width:96px; height:48px; background:var(--stone-edge); border-radius:var(--radius-md);"></div>
            <code style=${labelStyle}>--radius-md</code>
        </div>
        <div style=${rowStyle}>
            <div style="width:96px; height:48px; background:var(--stone-edge); border-radius:var(--radius-lg);"></div>
            <code style=${labelStyle}>--radius-lg</code>
        </div>
    </section>
`;
