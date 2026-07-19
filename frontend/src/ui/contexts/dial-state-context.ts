import { createContext } from "@lit/context";
import type { DialState } from "../ritual-dial";

export const dialStateContext = createContext<DialState>(Symbol("dial-state"));
