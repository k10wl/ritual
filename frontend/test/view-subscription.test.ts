import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { subscribeBeforeHydrate } from "../src/view-subscription.ts";
import { isNewerSnapshot } from "../src/vm-seq.ts";

describe("subscribeBeforeHydrate", () => {
	it("subscribes before starting snapshot hydration", async () => {
		const order: string[] = [];
		const { hydrate } = subscribeBeforeHydrate(
			(handler) => {
				order.push("subscribe");
				handler("event");
				return () => undefined;
			},
			(value) => order.push(`apply:${value}`),
			async () => {
				order.push("snapshot");
				return "snapshot";
			},
		);

		await hydrate;

		assert.deepEqual(order, [
			"subscribe",
			"apply:event",
			"snapshot",
			"apply:snapshot",
		]);
	});

	it("keeps a terminal Idle push when the older RPC snapshot finishes later", async () => {
		type Snapshot = { seq: number; phase: "preflight" | "idle" };
		let resolveSnapshot!: (snapshot: Snapshot) => void;
		let handler!: (snapshot: Snapshot) => void;
		let current: Snapshot = { seq: -1, phase: "preflight" };
		const apply = (incoming: Snapshot) => {
			if (isNewerSnapshot(current, incoming)) current = incoming;
		};

		const { hydrate } = subscribeBeforeHydrate(
			(next) => {
				handler = next;
				return () => undefined;
			},
			apply,
			() =>
				new Promise<Snapshot>((resolve) => {
					resolveSnapshot = resolve;
				}),
		);

		handler({ seq: 2, phase: "idle" });
		resolveSnapshot({ seq: 1, phase: "preflight" });
		await hydrate;

		assert.deepEqual(current, { seq: 2, phase: "idle" });
	});

	it("keeps the unsubscribe handle available while hydration is pending", async () => {
		let releaseSnapshot!: (value: string) => void;
		let unsubscribed = false;
		const { unsubscribe, hydrate } = subscribeBeforeHydrate(
			() => () => {
				unsubscribed = true;
			},
			() => undefined,
			() =>
				new Promise<string>((resolve) => {
					releaseSnapshot = resolve;
				}),
		);

		unsubscribe();
		assert.equal(unsubscribed, true);
		releaseSnapshot("snapshot");
		await hydrate;
	});
});
