// Thin wrapper over the auto-generated Wails bindings. Keeps the rest of
// the frontend decoupled from the binding directory layout.

import { Events } from "@wailsio/runtime";
import * as Control from "../bindings/ritual/internal/gui/control/controlservice";
import { Prep, SyncStatus } from "../bindings/ritual/internal/gui/control/models";
import { ViewModel, Stage, Phase, JoinAddress } from "../bindings/ritual/internal/gui/projection/models";
import { LogLine, Level } from "../bindings/ritual/internal/gui/logsink/models";

export { ViewModel, Stage, Phase, JoinAddress, LogLine, Level, Prep, SyncStatus };

export const start = (port: number, memoryMB: number) => Control.Start(port, memoryMB);
export const stop = () => Control.Stop();
export const dismiss = () => Control.Dismiss();
// Server-free sync gestures (design-log/031). The backend rejects either
// while another flow is Running; the IDLE-only render keeps them gated.
export const download = () => Control.Download();
export const upload = () => Control.Upload();
// Launch staleness check — boolean "behind". Errors degrade to a zero
// status backend-side, so this never rejects in practice.
export const getSyncStatus = () => Control.GetSyncStatus();
export const getSnapshot = () => Control.GetSnapshot();
export const getPrep = () => Control.GetPrep();
export const showLogs = () => Control.ShowLogs();
export const sendConsole = (line: string) => Control.SendConsole(line);
export const openRootFolder = () => Control.OpenRootFolder();

export function onView(handler: (vm: ViewModel) => void): () => void {
    return Events.On("ritual:view", (e) => handler(e.data));
}

export function onLog(handler: (line: LogLine) => void): () => void {
    return Events.On("log:line", (e) => handler(e.data));
}
