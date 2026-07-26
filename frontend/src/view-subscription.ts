export type SnapshotHandler<T> = (snapshot: T) => void;
export type Subscribe<T> = (handler: SnapshotHandler<T>) => () => void;

/**
 * Register the push listener before asking for the initial RPC snapshot.
 *
 * A pushed snapshot may arrive while the RPC is pending. Callers with ordered
 * snapshots apply their normal sequence guard in `apply` so an older RPC
 * response cannot overwrite that newer pushed state.
 */
export function subscribeBeforeHydrate<T>(
	subscribe: Subscribe<T>,
	apply: SnapshotHandler<T>,
	getSnapshot: () => Promise<T>,
): { unsubscribe: () => void; hydrate: Promise<void> } {
	const unsubscribe = subscribe(apply);
	const hydrate = getSnapshot()
		.then(apply)
		.catch(() => undefined);
	return { unsubscribe, hydrate };
}
