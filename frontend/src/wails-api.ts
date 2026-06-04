// Thin wrapper over the auto-generated Wails bindings. Keeps the rest of
// the frontend decoupled from the binding directory layout.

import { Events } from "@wailsio/runtime";
import * as Control from "../bindings/ritual/internal/gui/control/controlservice";
import { Prep, SyncStatus, Version, RetentionConfig } from "../bindings/ritual/internal/gui/control/models";
import { RetentionRules } from "../bindings/ritual/internal/core/domain/models";
import { ViewModel, Stage, Phase, JoinAddress } from "../bindings/ritual/internal/gui/projection/models";
import { ServerLog, ServerLogBatch, Level } from "../bindings/ritual/internal/gui/logsink/models";

export { ViewModel, Stage, Phase, JoinAddress, ServerLog, ServerLogBatch, Level, Prep, SyncStatus, Version, RetentionConfig, RetentionRules };

/** Version-history scope for ListVersions (design-log/038). */
export type VersionScope = "local" | "remote";

export const start = (port: number, memoryMB: number, skipSync = false) =>
    Control.Start(port, memoryMB, skipSync);
export const stop = () => Control.Stop();
export const dismiss = () => Control.Dismiss();
// Server-free sync gestures (design-log/031). The backend rejects either
// while another flow is Running; the IDLE-only render keeps them gated.
export const download = () => Control.Download();
export const upload = () => Control.Upload();
// World-save rollback (design-log/038). listVersions enumerates historical refs
// per scope (remote = canonical history, degrades to cached local); restore
// rolls the workdir back to the chosen ref. The backend rejects restore while
// another flow is Running.
export const listVersions = (scope: VersionScope) => Control.ListVersions(scope);
export const restore = (refID: string) => Control.Restore(refID);
// Retention rules (design-log/039). get returns the effective local+remote
// policy (zero sides normalised to defaults); set persists both — the next
// prune reads the file fresh, so edits apply without a restart.
export const getRetentionRules = () => Control.GetRetentionRules();
export const setRetentionRules = (local: RetentionRules, remote: RetentionRules) =>
    Control.SetRetentionRules(local, remote);
// Manual "Check for update" (design-log/037 §Q6). Runs the same Preflight flow
// as launch — the gray dial takes over. Frontend gates it to IDLE.
export const checkForUpdate = () => Control.CheckForUpdate();
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

// Server console batches (design-log/042). Go coalesces the MC console stream
// into one IPC per ~16ms; the handler appends each batch directly (no rAF).
export function onServerLogs(handler: (batch: ServerLogBatch) => void): () => void {
    return Events.On("server:logs", (e) => handler(e.data));
}
