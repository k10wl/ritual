import type { Tickable } from "./ripple";

export interface Clock {
    now(): number;
    raf(cb: FrameRequestCallback): number;
    caf(id: number): void;
}

const defaultClock: Clock = {
    now: () => performance.now(),
    raf: (cb) => requestAnimationFrame(cb),
    caf: (id) => cancelAnimationFrame(id),
};

export class Scheduler {
    private items: Tickable[] = [];
    private rafId = 0;
    private running = false;
    private clock: Clock;

    constructor(
        private onTick: () => void,
        clock: Clock = defaultClock,
    ) {
        this.clock = clock;
    }

    add(t: Tickable) {
        this.items.push(t);
        if (!this.running) this.startLoop();
    }

    stop() {
        this.clock.caf(this.rafId);
        this.running = false;
        this.items = [];
    }

    private startLoop() {
        this.running = true;
        const loop = () => {
            const now = this.clock.now();
            for (const it of this.items) if (!it.done) it.tick(now);
            this.items = this.items.filter((it) => !it.done);
            this.onTick();
            if (!this.items.length) {
                this.running = false;
                return;
            }
            this.rafId = this.clock.raf(loop);
        };
        this.rafId = this.clock.raf(loop);
    }
}
