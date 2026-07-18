import type { Rng } from "./rng";

export interface GlyphSource {
    next(): string;
}

export const DEFAULT_GLYPH_RANGES: ReadonlyArray<readonly [number, number]> = [
    [0x21, 94],     // printable ASCII
    [0x2500, 256],  // box drawing + block + geometric
    [0x2190, 112],  // arrows
    [0x2200, 256],  // math operators
];

export class RangeGlyphSource implements GlyphSource {
    constructor(
        private rng: Rng,
        private ranges: ReadonlyArray<readonly [number, number]> = DEFAULT_GLYPH_RANGES,
    ) {}
    next(): string {
        const [start, len] = this.ranges[this.rng.int(this.ranges.length)];
        return String.fromCharCode(start + this.rng.int(len));
    }
}
