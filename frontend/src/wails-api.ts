// Thin wrapper over the auto-generated Wails bindings. Keeps the rest of
// the frontend decoupled from the binding directory layout.

import { Events } from "@wailsio/runtime";
import * as Control from "../bindings/ritual/internal/gui/services/controlservice";
import { ViewModel, Stage, JoinAddress } from "../bindings/ritual/internal/gui/projection/models";
import { LogLine, Level } from "../bindings/ritual/internal/gui/logsink/models";

export { ViewModel, Stage, JoinAddress, LogLine, Level };

export const start = (port: number, memoryMB: number) => Control.Start(port, memoryMB);
export const stop = () => Control.Stop();
export const retry = () => Control.Retry();
export const getSnapshot = () => Control.GetSnapshot();
export const showLogs = () => Control.ShowLogs();
export const sendConsole = (line: string) => Control.SendConsole(line);
export const openRootFolder = () => Control.OpenRootFolder();

export function onView(handler: (vm: ViewModel) => void): () => void {
    return Events.On("ritual:view", (e) => handler(e.data));
}

export function onLog(handler: (line: LogLine) => void): () => void {
    return Events.On("log:line", (e) => handler(e.data));
}
