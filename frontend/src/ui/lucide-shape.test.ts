import { expect } from "@open-wc/testing";
import { Play, Square, X as XIcon, Download, Upload, BrainCog, Unplug } from "lucide";
import { shapeToD, type LucideIcon } from "./lucide-shape";

// Regression guard for design-log/050 §B: the dial's radial-button glyphs
// (play/stop/x/download/upload/brain-cog/unplug) are drawn by converting
// lucide's [tag, attrs] icon-node data through shapeToD, which only handles
// path/line/circle/rect tags. This test runs the REAL installed lucide data
// for every icon the dial actually uses through shapeToD and asserts every
// shape produces a non-empty path — so a future lucide upgrade that redraws
// one of these icons with an unhandled tag (polygon/polyline/ellipse) fails
// a test instead of silently shipping an invisible dial glyph.
const ICONS: Record<string, LucideIcon> = {
    play: Play as LucideIcon,
    stop: Square as LucideIcon,
    x: XIcon as LucideIcon,
    download: Download as LucideIcon,
    upload: Upload as LucideIcon,
    "brain-cog": BrainCog as LucideIcon,
    unplug: Unplug as LucideIcon,
};

describe("shapeToD over the real installed lucide icon data", () => {
    for (const [name, icon] of Object.entries(ICONS)) {
        it(`every shape in "${name}" converts to a non-empty path`, () => {
            expect(icon.length, `"${name}" has no shapes at all`).to.be.greaterThan(0);
            icon.forEach(([tag, attrs], i) => {
                const d = shapeToD([tag, attrs]);
                expect(
                    d,
                    `"${name}" shape #${i} (tag="${tag}") produced an empty path — shapeToD ` +
                        "doesn't handle this tag (design-log/050 §B)",
                ).to.not.equal("");
            });
        });
    }
});
