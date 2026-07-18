/**
 * Wails API context — provides the service surface to deep consumers so
 * primitives and components do not import `wails-api` directly. Provider
 * lives in `ritual-app` (production binding) or in Storybook decorators
 * (stubbed transport).
 */

import { createContext } from "@lit/context";
import * as wailsApi from "../../wails-api";

export type WailsApi = typeof wailsApi;

export const wailsApiContext = createContext<WailsApi>(Symbol("wails-api"));
