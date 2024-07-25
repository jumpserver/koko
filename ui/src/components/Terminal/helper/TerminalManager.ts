import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { ILunaConfig } from '@/components/interface';
import { createDiscreteApi } from 'naive-ui';
import { ref, Ref } from 'vue';

const { message } = createDiscreteApi(['message']);

class TerminalManager {
    public term: Terminal | null = null;
    private fitAddon: FitAddon | null = null;
    private lunaId: Ref<string> = ref('');
    private origin: string | null = null;

    private readonly config: ILunaConfig;

    constructor() {
        this.config = this.loadConfig();
    }

    /**
     * @description 获取 Luna 配置
     */
    private loadLunaConfig(): ILunaConfig {
        const config: ILunaConfig = {};
        let fontSize = 14;
        let quickPaste = '0';
        let backspaceAsCtrlH = '0';
        let localSettings = localStorage.getItem('LunaSetting');

        if (localSettings !== null) {
            let settings = JSON.parse(localSettings);
            let commandLine = settings['command_line'];
            if (commandLine) {
                fontSize = commandLine['character_terminal_font_size'];
                quickPaste = commandLine['is_right_click_quickly_paste'] ? '1' : '0';
                backspaceAsCtrlH = commandLine['is_backspace_as_ctrl_h'] ? '1' : '0';
            }
        }
        if (!fontSize || fontSize < 5 || fontSize > 50) {
            fontSize = 13;
        }

        config['fontSize'] = fontSize;
        config['quickPaste'] = quickPaste;
        config['backspaceAsCtrlH'] = backspaceAsCtrlH;
        config['ctrlCAsCtrlZ'] = '0';

        return config;
    }

    /**
     * @description 用户的操作系统类型设置行高
     */
    private loadConfig(): ILunaConfig {
        const config = this.loadLunaConfig();
        const ua = navigator.userAgent.toLowerCase();
        let lineHeight = 1;
        if (ua.indexOf('windows') !== -1) {
            lineHeight = 1.2;
        }
        config['lineHeight'] = lineHeight;
        return config;
    }

    /**
     * @description 与 Luna 通信
     */
    private sendEventToLuna(name: string, data: string) {
        if (this.lunaId.value != null && this.origin != null) {
            window.parent.postMessage(
                { name: name, id: this.lunaId.value, data: data },
                this.origin
            );
        }
    }

    /**
     * @description 处理视口大小变化
     * @param {FitAddon} fitAddon
     * @param {Terminal} term
     */
    private handleResize(fitAddon: FitAddon, term: Terminal) {
        fitAddon.fit();
        console.log(`Windows resize event, ${term.cols}, ${term.rows}, ${term}`);
    }

    /**
     * @description 处理鼠标移入
     */
    private handleMouseenter(term: Terminal) {
        term.focus();
    }

    /**
     * @description 处理 Terminal 实例 changeSelect 事件
     * @param term
     * @param termSelectionTextRef
     */
    private handleSelectionChange(term: Terminal, termSelectionTextRef: Ref<string>) {
        const termSelectionText: string = term.getSelection().trim();
        message.info('select change');
        navigator.clipboard.writeText(termSelectionText).then();
        termSelectionTextRef.value = termSelectionText;
    }

    /**
     * @description 处理 Terminal 中键盘事件
     * @param {KeyboardEvent} e
     * @param {Terminal} term
     */
    private handleCustomKeyEvent(e: KeyboardEvent, term: Terminal) {
        if (e.altKey && (e.key === 'ArrowRight' || e.key === 'ArrowLeft')) {
            switch (e.key) {
                case 'ArrowRight':
                    this.sendEventToLuna('KEYEVENT', 'alt+right');
                    break;
                case 'ArrowLeft':
                    this.sendEventToLuna('KEYEVENT', 'alt+left');
                    break;
            }
        }

        if (e.ctrlKey && e.key === 'c' && term.hasSelection()) {
            return false;
        }

        return !(e.ctrlKey && e.key === 'v');
    }

    // todo)) wsIsActivated 和 ws
    private handleConextMenu(e: MouseEvent, config: ILunaConfig, termSelectionText: string) {
        if (e.ctrlKey || config.quickPaste !== '1') return;

        if (navigator.clipboard && navigator.clipboard.readText) {
            navigator.clipboard.readText().then(text => {
                console.log(text);
                // if (this.wsIsActivated()) {
                //     this.ws.send(this.message(this.terminalId, 'TERMINAL_DATA', text))
                // }
            });
            e.preventDefault();
        } else if (termSelectionText !== '') {
            // if (this.wsIsActivated()) {
            //     this.ws.send(this.message(this.terminalId, 'TERMINAL_DATA', this.termSelectionText))
            // }
            e.preventDefault();
        }
    }

    /**
     *@description 创建 Terminal
     */
    public createTerminal(el: HTMLElement): Promise<{ termSelectionText: string }> {
        this.fitAddon = new FitAddon();

        const term = new Terminal({
            fontSize: this.config.fontSize,
            lineHeight: this.config.lineHeight,
            fontFamily: 'monaco, Consolas, "Lucida Console", monospace',
            rightClickSelectsWord: true,
            theme: {
                background: '#1E1E1E'
            },
            scrollback: 5000
        });

        term.open(el);
        this.fitAddon.fit();
        term.focus();
        // term.write('Hello from \x1B[1;3;31mxterm.js\x1B[0m $ ');

        const termSelectionTextRef = ref<string>('');

        term.onSelectionChange(() => this.handleSelectionChange(term, termSelectionTextRef));
        term.attachCustomKeyEventHandler(($event: KeyboardEvent) =>
            this.handleCustomKeyEvent($event, term)
        );

        el.addEventListener('mouseenter', () => this.handleMouseenter(term));
        el.addEventListener('contextmenu', async ($event: MouseEvent) =>
            this.handleConextMenu($event, this.config, termSelectionTextRef.value)
        );

        window.addEventListener('resize', () => this.handleResize(this.fitAddon!, term));

        this.term = term;

        return new Promise(resolve => {
            resolve({
                termSelectionText: termSelectionTextRef.value
            });
        });
    }

    /**
     * @description 处理来自 Luna 的事件
     */
    public handleEventFromLuna(evt: MessageEvent) {
        const msg = evt.data;
        switch (msg.name) {
            case 'PING':
                if (this.lunaId.value != null) {
                    return;
                }
                this.lunaId.value = msg.id;
                this.origin = evt.origin;
                this.sendEventToLuna('PONG', '');
                break;
            case 'CMD':
                // sendDataFromWindow(msg.data); // You need to implement sendDataFromWindow
                break;
            case 'FOCUS':
                if (this.term) {
                    this.term.focus();
                }
                break;
            case 'OPEN':
                // this.$emit('event', 'open', this.terminalId); // Uncomment if you have a proper emit method
                break;
        }
        console.log('KoKo got post message: ', msg);
    }
}

export default TerminalManager;
