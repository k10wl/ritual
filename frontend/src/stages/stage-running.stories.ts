import { html } from "lit";
import "./stage-running";
import { ViewModel, Stage, JoinAddress } from "../wails-api";

interface Args {
    readyLight: boolean;
    addressCount: number;
}

const ALL_ADDRESSES = [
    new JoinAddress({ label: "LAN", address: "192.168.1.10:25565" }),
    new JoinAddress({ label: "Tailscale", address: "100.64.0.5:25565" }),
    new JoinAddress({ label: "Hamachi", address: "25.42.7.118:25565" }),
];

export default {
    title: "Stages/Running",
    component: "stage-running",
    argTypes: {
        readyLight: { control: { type: "boolean" } },
        addressCount: { control: { type: "range", min: 0, max: 3, step: 1 } },
    },
    args: { readyLight: true, addressCount: 2 },
};

export const Snapshot = (args: Args) => html`
    <stage-running
        .vm=${new ViewModel({
            stage: Stage.StageRunning,
            readyLight: args.readyLight,
            addresses: ALL_ADDRESSES.slice(0, args.addressCount),
        })}
    ></stage-running>
`;
