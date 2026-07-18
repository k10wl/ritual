export interface Rng {
    next(): number;
    int(maxExclusive: number): number;
    range(min: number, max: number): number;
}

export class SeededRng implements Rng {
    private s: number;
    constructor(seed: number) {
        this.s = seed >>> 0;
    }
    next(): number {
        let t = (this.s = (this.s + 0x6d2b79f5) >>> 0);
        t = Math.imul(t ^ (t >>> 15), t | 1);
        t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
        return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    }
    int(n: number): number {
        return Math.floor(this.next() * n);
    }
    range(lo: number, hi: number): number {
        return lo + this.next() * (hi - lo);
    }
}

export const cryptoSeed = (): number => {
    const buf = new Uint32Array(1);
    globalThis.crypto.getRandomValues(buf);
    return buf[0];
};
