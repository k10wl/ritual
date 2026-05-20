import { html } from "lit";
import "../ritual-app";

export default {
    title: "App/Ritual",
    component: "ritual-app",
    parameters: { frame: "bare" },
};

export const Live = () => html`<ritual-app></ritual-app>`;
