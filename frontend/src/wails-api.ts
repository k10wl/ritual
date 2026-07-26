// Thin wrapper over the auto-generated Wails bindings. Keeps the rest of
// the frontend decoupled from the binding directory layout.

import { Events } from "@wailsio/runtime";
import * as ControlRaw from "../bindings/ritual/internal/gui/control/controlservice";
import {
	Prep,
	SyncStatus,
	Version,
	RetentionConfig,
	LocalStorageStats,
} from "../bindings/ritual/internal/gui/control/models";
import { RetentionRules } from "../bindings/ritual/internal/core/domain/models";
import {
	ViewModel,
	Stage,
	Phase,
	JoinAddress,
} from "../bindings/ritual/internal/gui/projection/models";
import {
	ServerLog,
	ServerLogBatch,
	Level,
} from "../bindings/ritual/internal/gui/logsink/models";

export {
	ViewModel,
	Stage,
	Phase,
	JoinAddress,
	ServerLog,
	ServerLogBatch,
	Level,
	Prep,
	SyncStatus,
	Version,
	RetentionConfig,
	RetentionRules,
	LocalStorageStats,
};

// Wails IPC echo — permanent, always-on observability, both directions.
//
// IN: wraps window._wails.dispatchWailsEvent, the exact function native Go
// code calls into the JS engine for every event on every window, before any
// app-level listener (onView, onServerLogs, etc.) runs — so the devtools
// console shows unconditionally whether a given event (e.g. a terminal
// "ritual:view" snapshot) ever reached the JS engine at all. Importing
// "@wailsio/runtime" above has already run events.js's top-level code, which
// sets window._wails.dispatchWailsEvent — so it's safe to wrap here.
if (typeof window !== "undefined") {
	const w = window as unknown as {
		_wails?: {
			dispatchWailsEvent?: (event: { name: string; data: unknown }) => void;
		};
	};
	const original = w._wails?.dispatchWailsEvent;
	if (typeof original === "function") {
		w._wails!.dispatchWailsEvent = (event) => {
			console.log(
				`[wails-event IN ${new Date().toISOString()}] ${event?.name}`,
				event?.data,
			);
			return original(event);
		};
	} else {
		console.warn(
			"[wails-event echo] window._wails.dispatchWailsEvent not found at wails-api.ts load time",
		);
	}
}

// OUT: a Proxy over the generated ControlService bindings logs every outgoing
// call (method + args) and its eventual result/error as a passive .then()
// subscription — the original return value (a CancellablePromise, incl. its
// .cancel()) is handed back to the caller completely unmodified, only
// observed. Every existing `Control.X(...)` call below transparently goes
// through this — no per-method wiring, and any future binding is covered too.
function echoCalls<T extends object>(target: T, label: string): T {
	// Do not proxy the generated ES module namespace object directly. Module
	// namespace exports are read-only/non-configurable, and proxy invariants
	// require `get` to return the exact original function for those slots.
	// Copy onto a plain object first so wrapping function values is legal.
	return new Proxy(
		{ ...target },
		{
			get(obj, prop, receiver) {
				const value = Reflect.get(obj, prop, receiver);
				if (typeof value !== "function") return value;
				return (...args: unknown[]) => {
					const method = String(prop);
					console.log(
						`[wails-call OUT ${new Date().toISOString()}] ${label}.${method}`,
						args,
					);
					const result = Reflect.apply(value, obj, args);
					const maybePromise = result as {
						then?: (
							onFulfilled: (v: unknown) => void,
							onRejected: (e: unknown) => void,
						) => void;
					};
					if (maybePromise && typeof maybePromise.then === "function") {
						maybePromise.then(
							(v) =>
								console.log(
									`[wails-call RESULT ${new Date().toISOString()}] ${label}.${method}`,
									v,
								),
							(e) =>
								console.log(
									`[wails-call ERROR ${new Date().toISOString()}] ${label}.${method}`,
									e,
								),
						);
					} else {
						console.log(
							`[wails-call RESULT ${new Date().toISOString()}] ${label}.${method}`,
							result,
						);
					}
					return result;
				};
			},
		},
	);
}
const Control = echoCalls(ControlRaw, "Control");

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
export const listVersions = (scope: VersionScope) =>
	Control.ListVersions(scope);
export const restore = (refID: string) => Control.Restore(refID);
// Per-version delete (design-log/045 §A + post-ship remote-delete extension,
// user direction 2026-06-05). Local: drops refs/<id>.json + GCs orphan blobs;
// clears settings.LoadedRefID if the deleted id was loaded so the "current"
// badge falls back to IsHead. Remote: same shape on the canonical store,
// does not touch local state or LoadedRefID.
export const deleteLocalVersion = (refID: string) =>
	Control.DeleteLocalVersion(refID);
export const deleteRemoteVersion = (refID: string) =>
	Control.DeleteRemoteVersion(refID);
// Revert workdir to local HEAD (design-log/045 §C). Drops uncommitted edits;
// observable no-op when unpushed-only. Rejected while another flow runs.
export const revert = () => Control.Revert();
// Local on-disk stats (design-log/045 §E). Dedup-aware byte sum + object
// count under local objects/. Cached 5s; invalidates on delete/apply.
export const getLocalStorageStats = () => Control.GetLocalStorageStats();
// Retention rules (design-log/039). get returns the effective local+remote
// policy (zero sides normalised to defaults); set persists both — the next
// prune reads the file fresh, so edits apply without a restart.
export const getRetentionRules = () => Control.GetRetentionRules();
export const setRetentionRules = (
	local: RetentionRules,
	remote: RetentionRules,
) => Control.SetRetentionRules(local, remote);
// Apply retention now (design-log/045 §D). Runs the local + remote retention
// jobs in one beat. Settings must already be persisted (via setRetentionRules)
// so the prune reads the freshly-applied policy.
export const applyRetentionNow = () => Control.ApplyRetentionNow();
// Manual "Check for update" (design-log/037 §Q6). Runs the same Preflight flow
// as launch — the gray dial takes over. Frontend gates it to IDLE.
export const checkForUpdate = () => Control.CheckForUpdate();
// Launch staleness check — boolean "behind". Errors degrade to a zero
// status backend-side, so this never rejects in practice.
export const getSyncStatus = () => Control.GetSyncStatus();
export const getSnapshot = () => Control.GetSnapshot();
export const getPrep = () => Control.GetPrep();
export const showLogs = () => Control.ShowLogs();
// One-shot server-console backfill (design-log/043): the tail of the running
// server's own latest.log, raw lines newest-last. The logs window calls this
// once on open, before switching to the live server:logs wire.
export const readServerLog = () => Control.ReadServerLog();
export const sendConsole = (line: string) => Control.SendConsole(line);
export const openRootFolder = () => Control.OpenRootFolder();

export function onView(handler: (vm: ViewModel) => void): () => void {
	return Events.On("ritual:view", (e) => handler(e.data));
}

// Server console batches (design-log/042). Go coalesces the MC console stream
// into one IPC per ~16ms; the handler appends each batch directly (no rAF).
export function onServerLogs(
	handler: (batch: ServerLogBatch) => void,
): () => void {
	return Events.On("server:logs", (e) => handler(e.data));
}
