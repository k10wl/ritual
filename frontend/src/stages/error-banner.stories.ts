import { html } from "lit";
import "./error-banner";
import { ViewModel, Stage } from "../wails-api";

interface Args {
    errorText: string;
}

export default {
    title: "Stages/ErrorBanner",
    component: "error-banner",
    argTypes: {
        errorText: { control: { type: "text" } },
    },
    args: { errorText: "R2 upload failed: connection reset" },
};

export const Snapshot = (args: Args) => html`
    <error-banner
        .vm=${new ViewModel({ stage: Stage.StageFailed, errorText: args.errorText })}
    ></error-banner>
`;
