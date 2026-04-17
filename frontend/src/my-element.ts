import {css, html, LitElement} from 'lit'
import {customElement, property} from 'lit/decorators.js'
import {Events} from "@wailsio/runtime";
import {GreetService, SysInfoService, NetInfoService} from '../bindings/ritual/internal/gui/services';

@customElement('my-element')
export class MyElement extends LitElement {

    @property()
    result: string = 'Please enter your name below'

    @property()
    time: string = '—'

    @property()
    name: string = ''

    @property()
    ramLine: string = 'RAM: —'

    @property({attribute: false})
    ips: {label: string, address: string}[] = []

    private ramTimer?: number
    private ipsTimer?: number
    private onVisibility = () => this.syncIpsPolling()
    private onFocus = () => this.syncIpsPolling()
    private onBlur = () => this.syncIpsPolling()

    constructor() {
        super();
        Events.On('time', (timeValue: { data: string }) => {
            this.time = timeValue.data;
        });
    }

    connectedCallback() {
        super.connectedCallback();
        this.refreshRAM();
        this.ramTimer = window.setInterval(() => this.refreshRAM(), 1000);

        document.addEventListener('visibilitychange', this.onVisibility);
        window.addEventListener('focus', this.onFocus);
        window.addEventListener('blur', this.onBlur);
        this.syncIpsPolling();
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        if (this.ramTimer) clearInterval(this.ramTimer);
        this.stopIpsPolling();
        document.removeEventListener('visibilitychange', this.onVisibility);
        window.removeEventListener('focus', this.onFocus);
        window.removeEventListener('blur', this.onBlur);
    }

    private shouldPollIps(): boolean {
        return !document.hidden && document.hasFocus();
    }

    private syncIpsPolling() {
        if (this.shouldPollIps()) this.startIpsPolling();
        else this.stopIpsPolling();
    }

    private startIpsPolling() {
        if (this.ipsTimer) return;
        this.refreshIps();
        this.ipsTimer = window.setInterval(() => this.refreshIps(), 1000);
    }

    private stopIpsPolling() {
        if (!this.ipsTimer) return;
        clearInterval(this.ipsTimer);
        this.ipsTimer = undefined;
    }

    async refreshIps() {
        try {
            const payload = await NetInfoService.JoinAddresses();
            this.ips = payload.addresses;
        } catch (err) {
            console.log('ips err', err);
        }
    }

    async refreshRAM() {
        try {
            const ram = await SysInfoService.GetRAM();
            this.ramLine = `RAM: ${ram.usedMB} / ${ram.totalMB} MB (${ram.usedPercent.toFixed(1)}%)`;
        } catch (err) {
            this.ramLine = `RAM: err ${err}`;
        }
    }

    doGreet() {
        const name = this.name || 'anonymous';
        GreetService.Greet(name).then((resultValue: string) => {
            this.result = resultValue;
        }).catch((err: Error) => {
            console.log(err);
        });
    }

    render() {
        return html`
            <div class="container">
                <h1>Ritual</h1>
                <div aria-label="result" class="result">${this.result}</div>
                <div class="card">
                    <div class="input-box">
                        <input aria-label="input" class="input" .value=${this.name}
                               @input=${(e: InputEvent) => this.name = (e.target as HTMLInputElement).value}
                               type="text" autocomplete="off"/>
                        <button aria-label="greet-btn" class="btn" @click=${this.doGreet}>Greet</button>
                    </div>
                </div>
                <div class="ips" aria-label="join-addresses">
                    <div class="ips-title">Join addresses</div>
                    ${this.ips.length === 0
                        ? html`<div class="ips-empty">—</div>`
                        : html`<ul class="ips-list">
                            ${this.ips.map(ip => html`<li><span class="ips-label">${ip.label}</span> ${ip.address}</li>`)}
                        </ul>`}
                </div>
                <div class="stats">
                    <div>${this.ramLine}</div>
                    <div>${this.time}</div>
                </div>
            </div>
        `
    }

    static styles = css`
        :host {
            max-width: 1280px;
            margin: 0 auto;
            padding: 2rem;
            text-align: center;
            display: block;
        }
        h1 {
            font-size: 2.5em;
            margin: 0 0 1rem;
        }
        .container {
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
        }
        .result {
            height: 20px;
            line-height: 20px;
            margin: 1rem auto;
        }
        .input-box {
            display: flex;
            gap: 0.5rem;
        }
        .input-box .input {
            border: none;
            border-radius: 3px;
            outline: none;
            height: 30px;
            line-height: 30px;
            padding: 0 10px;
            color: black;
            background: rgba(240, 240, 240, 1);
        }
        .input-box .btn {
            width: 70px;
            height: 30px;
            line-height: 30px;
            border-radius: 3px;
            border: none;
            cursor: pointer;
        }
        .ips {
            margin-top: 1.5rem;
            font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
            font-size: 0.95em;
        }
        .ips-title {
            font-weight: 600;
            margin-bottom: 0.25rem;
            opacity: 0.85;
        }
        .ips-empty {
            opacity: 0.6;
        }
        .ips-list {
            list-style: none;
            margin: 0;
            padding: 0;
            display: flex;
            flex-direction: column;
            gap: 0.15rem;
        }
        .ips-label {
            font-weight: 600;
            opacity: 0.85;
        }
        .ips-label::after {
            content: ':';
            margin-right: 0.4em;
        }
        .stats {
            margin-top: 1.5rem;
            font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
            font-size: 0.9em;
            opacity: 0.85;
            display: flex;
            flex-direction: column;
            gap: 0.25rem;
        }
    `;
}

declare global {
    interface HTMLElementTagNameMap {
        'my-element': MyElement
    }
}
