const WS_RE = /\s/;

export class Cell {
    target: string;
    glyph: string;
    scrambling = false;

    constructor(target: string, glyph?: string) {
        this.target = target;
        this.glyph = glyph ?? target;
    }

    get inert(): boolean {
        return WS_RE.test(this.target);
    }

    retarget(next: string) {
        this.target = next;
        if (WS_RE.test(next)) this.glyph = next;
    }
}
