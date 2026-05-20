// Storybook helper. Mounts a stage element whose .vm prop either holds a
// fixed snapshot or ramps progress from `start` toward 100 at one percent
// per `speedMsPerPct` milliseconds, looping back to 0. The loop self-stops
// when the element leaves the document so story switches do not leak.
export function buildStage(
    elementName: string,
    args: {
        animated: boolean;
        speedMsPerPct: number;
        start: number;
        vmAt: (progress: number) => unknown;
    },
): HTMLElement {
    const el = document.createElement(elementName) as HTMLElement & { vm?: unknown };
    el.vm = args.vmAt(args.start);
    if (!args.animated) return el;

    let p = args.start;
    const tick = () => {
        if (!document.body.contains(el)) return;
        p = p < 100 ? p + 1 : 0;
        el.vm = args.vmAt(p);
        window.setTimeout(tick, args.speedMsPerPct);
    };
    window.setTimeout(tick, args.speedMsPerPct);
    return el;
}
