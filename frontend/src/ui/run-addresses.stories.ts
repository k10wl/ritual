import { html } from "lit";
import "./run-addresses";
import { JoinAddress } from "../wails-api";

const ALL_ADDRESSES = [
    new JoinAddress({ label: "localhost", address: "127.0.0.1:25565" }),
    new JoinAddress({ label: "Wi-Fi", address: "192.168.1.42:25565" }),
    new JoinAddress({ label: "Ethernet", address: "10.0.0.7:25565" }),
    new JoinAddress({ label: "Tailscale", address: "100.64.0.5:25565" }),
];

interface Args {
    addressCount: number;
}

export default {
    title: "Run / Addresses",
    component: "run-addresses",
    argTypes: {
        addressCount: { control: { type: "range", min: 0, max: ALL_ADDRESSES.length, step: 1 } },
    },
    args: { addressCount: 3 },
};

export const Playground = (a: Args) => html`
    <div style="padding:24px; display:flex; justify-content:center;">
        <run-addresses
            .addresses=${ALL_ADDRESSES.slice(0, a.addressCount)}
        ></run-addresses>
    </div>
`;

export const Empty = () => html`
    <div style="padding:24px; color:#94a3b8;">
        Empty list renders nothing — guard for completeness.
        <div style="margin-top:12px;">
            <run-addresses .addresses=${[]}></run-addresses>
        </div>
    </div>
`;

export const SingleLocalhost = () => html`
    <div style="padding:24px; display:flex; justify-content:center;">
        <run-addresses .addresses=${ALL_ADDRESSES.slice(0, 1)}></run-addresses>
    </div>
`;

export const FullList = () => html`
    <div style="padding:24px; display:flex; justify-content:center;">
        <run-addresses .addresses=${ALL_ADDRESSES}></run-addresses>
    </div>
`;
