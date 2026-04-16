import {css, html, LitElement} from 'lit'
import {customElement, property} from 'lit/decorators.js'
import {Events} from "@wailsio/runtime";
import {GreetService, SysInfoService} from '../bindings/ritual/internal/gui/services';

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

    private ramTimer?: number

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
    }

    disconnectedCallback() {
        super.disconnectedCallback();
        if (this.ramTimer) clearInterval(this.ramTimer);
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
