import "./stage-downloading";
import { ViewModel, Stage } from "../wails-api";
import { buildStage } from "./_anim";

interface Args {
    animated: boolean;
    speedMsPerPct: number;
    progress: number;
    bytesTotal: number;
    label: string;
}

export default {
    title: "Stages/Downloading",
    component: "stage-downloading",
    argTypes: {
        animated: { control: { type: "boolean" } },
        speedMsPerPct: {
            name: "ms / 1% step",
            control: { type: "range", min: 20, max: 500, step: 10 },
        },
        progress: {
            control: { type: "range", min: 0, max: 100, step: 1 },
            if: { arg: "animated", truthy: false },
        },
        bytesTotal: { control: { type: "number" } },
        label: { control: { type: "text" } },
    },
    args: {
        animated: false,
        speedMsPerPct: 90,
        progress: 42,
        bytesTotal: 1_000_000_000,
        label: "Downloading world…",
    },
};

const vmAt = (a: Args) => (p: number) =>
    new ViewModel({
        stage: Stage.StageDownloading,
        progress: p,
        bytesDone: a.bytesTotal > 0 ? Math.round((a.bytesTotal * p) / 100) : 0,
        bytesTotal: a.bytesTotal,
        label: a.label,
    });

export const Snapshot = (args: Args) =>
    buildStage("stage-downloading", {
        animated: args.animated,
        speedMsPerPct: args.speedMsPerPct,
        start: args.progress,
        vmAt: vmAt(args),
    });
