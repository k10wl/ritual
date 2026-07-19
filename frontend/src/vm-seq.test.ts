import { expect } from "@open-wc/testing";
import { isNewerSnapshot } from "./vm-seq";

describe("isNewerSnapshot (design-log/051 Q11)", () => {
    it("accepts a strictly greater seq", () => {
        expect(isNewerSnapshot({ seq: 1 }, { seq: 2 })).to.be.true;
    });

    it("rejects an equal seq (duplicate delivery)", () => {
        expect(isNewerSnapshot({ seq: 3 }, { seq: 3 })).to.be.false;
    });

    it("rejects a lesser seq — the exact late-straggler race from the captured repro", () => {
        // Go submitted saving(seq=5) before idle(seq=6), but the "saving"
        // duplicate executed in the browser after idle already landed.
        expect(isNewerSnapshot({ seq: 6 }, { seq: 5 })).to.be.false;
    });

    it("a fresh placeholder (seq -1) always loses to any real snapshot, including seq 0", () => {
        // Backend Seq is a Go zero-value (0) before Projection.Run's first
        // emit; FALLBACK_VM uses -1 so that narrow window still applies
        // correctly instead of being mistaken for a duplicate/stale value.
        expect(isNewerSnapshot({ seq: -1 }, { seq: 0 })).to.be.true;
    });
});
