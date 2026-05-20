import { html } from "lit";
import "./stage-locked";
import { ViewModel, Stage } from "../wails-api";

interface Args {
    lockHolder: string;
}

export default {
    title: "Stages/Locked",
    component: "stage-locked",
    argTypes: {
        lockHolder: { control: { type: "text" } },
    },
    args: { lockHolder: "alice" },
};

export const Snapshot = (args: Args) => html`
    <stage-locked
        .vm=${new ViewModel({ stage: Stage.StageLocked, lockHolder: args.lockHolder })}
    ></stage-locked>
`;
