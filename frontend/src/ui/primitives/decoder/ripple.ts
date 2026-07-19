import type { Cell } from "./cell";
import type { GlyphSource } from "./glyphs";
import type { Rng } from "./rng";

export interface Tickable {
    tick(nowMs: number): void;
    readonly done: boolean;
}

export interface RippleSpec {
    center: number;
    radius: number;
    rounds: number | readonly [number, number];
    tickDurationMs: number;
    lengthRush?: boolean;
}

interface CellSchedule {
    cell: Cell;
    startAt: number;
    rounds: number;
    lastTick: number;
}

const LENGTH_RUSH_MAX = 3;

const pickRounds = (rounds: RippleSpec["rounds"], rng: Rng): number => {
    if (typeof rounds === "number") return rounds;
    const [lo, hi] = rounds;
    return rng.int(hi - lo + 1) + lo;
};

const roundsUpperBound = (rounds: RippleSpec["rounds"]): number =>
    typeof rounds === "number" ? rounds : rounds[1];

export class Ripple implements Tickable {
    private schedules: CellSchedule[] = [];
    private finished = false;
    readonly spec: RippleSpec;

    constructor(
        spec: RippleSpec,
        cells: Cell[],
        rng: Rng,
        private glyphs: GlyphSource,
        nowMs: number,
    ) {
        this.spec = spec;
        const lo = Math.max(0, spec.center - spec.radius);
        const hi = Math.min(cells.length - 1, spec.center + spec.radius);

        const eligible: { cell: Cell; distance: number }[] = [];
        for (let i = lo; i <= hi; i++) {
            const cell = cells[i];
            if (!cell) continue;
            if (cell.inert) continue;
            if (cell.scrambling) continue;
            eligible.push({ cell, distance: Math.abs(i - spec.center) });
        }

        if (spec.lengthRush) {
            this.buildRushSchedule(eligible, spec, rng, nowMs);
        } else {
            this.buildWaveSchedule(eligible, spec, rng, nowMs);
        }

        if (!this.schedules.length) this.finished = true;
    }

    private buildWaveSchedule(
        eligible: { cell: Cell; distance: number }[],
        spec: RippleSpec,
        rng: Rng,
        nowMs: number,
    ) {
        for (const e of eligible) {
            const rounds = pickRounds(spec.rounds, rng);
            if (rounds <= 0) continue;
            this.schedules.push({
                cell: e.cell,
                startAt: nowMs + e.distance * spec.tickDurationMs,
                rounds,
                lastTick: -1,
            });
        }
    }

    private buildRushSchedule(
        eligible: { cell: Cell; distance: number }[],
        spec: RippleSpec,
        rng: Rng,
        nowMs: number,
    ) {
        if (!eligible.length) return;
        const budget = Math.max(1, Math.min(LENGTH_RUSH_MAX, roundsUpperBound(spec.rounds)));
        const charsPerRound = Math.max(1, Math.ceil(eligible.length / budget));
        eligible.sort((a, b) => a.distance - b.distance);
        for (let k = 0; k < eligible.length; k++) {
            const tickOffset = Math.floor(k / charsPerRound);
            const remainingBudget = budget - tickOffset;
            const rounds = Math.min(pickRounds(spec.rounds, rng), remainingBudget);
            if (rounds <= 0) continue;
            this.schedules.push({
                cell: eligible[k].cell,
                startAt: nowMs + tickOffset * spec.tickDurationMs,
                rounds,
                lastTick: -1,
            });
        }
    }

    tick(nowMs: number) {
        let pending = 0;
        for (const s of this.schedules) {
            if (s.rounds < 0) continue;
            if (s.cell.inert) {
                s.rounds = -1;
                continue;
            }
            if (nowMs < s.startAt) {
                pending++;
                continue;
            }
            const t = Math.floor((nowMs - s.startAt) / this.spec.tickDurationMs);
            if (t >= s.rounds) {
                s.cell.glyph = s.cell.target;
                s.cell.scrambling = false;
                s.rounds = -1;
                continue;
            }
            if (t !== s.lastTick) {
                s.cell.glyph = this.glyphs.next();
                s.cell.scrambling = true;
                s.lastTick = t;
            }
            pending++;
        }
        if (!pending && !this.finished) {
            this.settleAll();
            this.finished = true;
        }
    }

    private settleAll() {
        for (const s of this.schedules) {
            s.cell.glyph = s.cell.target;
            s.cell.scrambling = false;
        }
    }

    get done(): boolean {
        return this.finished;
    }
}
