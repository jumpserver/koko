import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { ILunaConfig } from '@/components/interface';
import { createDiscreteApi } from 'naive-ui';

const { message } = createDiscreteApi(['message']);

import { ref, Ref } from 'vue';
/**
 * @description 获取 Luna 配置
 */
const loadLunaConfig = () => {
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
};

/**
 * @description 处理视口大小变化
 * @param {FitAddon} fitAddon
 * @param {Terminal} term
 */
const handleResize = (fitAddon: FitAddon, term: Terminal) => {
    fitAddon.fit();

    // message.info(`Windows resize event, ${term.cols}, ${term.rows}, ${term}`);
};

/**
 * @description 处理鼠标移入
 */
const handleMouseenter = (term: Terminal) => {
    term.focus();
};

/**
 * @description 处理 Terminal 实例 changeSelect 事件
 * @param term
 * @param termSelectionTextRef
 */
const handleSelectionChange = (term: Terminal, termSelectionTextRef: Ref) => {
    const termSelectionText: string = term.getSelection().trim();

    message.info('select change');

    navigator.clipboard.writeText(termSelectionText).then();

    termSelectionTextRef.value = termSelectionText;
};

const handleConextMenu = (e: MouseEvent, config: ILunaConfig, termSelectionText: string) => {
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
};

/**
 * @description 用户的操作系统类型设置行高
 */
const loadConfig = () => {
    const config: ILunaConfig = loadLunaConfig();
    const ua = navigator.userAgent.toLowerCase();
    let lineHeight = 1;
    if (ua.indexOf('windows') !== -1) {
        lineHeight = 1.2;
    }
    config['lineHeight'] = lineHeight;
    return config;
};

/**
 *@description 创建 Terminal
 */
const createTerminal = (config: ILunaConfig, el: HTMLElement, fitAddon: FitAddon) => {
    let lineHeight = config.lineHeight;
    let fontSize = config.fontSize;

    const term = new Terminal({
        fontSize: fontSize,
        lineHeight: lineHeight,
        fontFamily: 'monaco, Consolas, "Lucida Console", monospace',
        rightClickSelectsWord: true,
        theme: {
            background: '#1E1E1E'
        },
        scrollback: 5000
    });

    term.open(el);
    fitAddon.fit();
    term.focus();
    term.write('Hello from \x1B[1;3;31mxterm.js\x1B[0m $ ');
    const termSelectionTextRef = ref<string>('');

    term.onSelectionChange(() => handleSelectionChange(term, termSelectionTextRef));

    el.addEventListener('mouseenter', () => handleMouseenter(term));
    el.addEventListener('contextmenu', async ($event: MouseEvent) =>
        handleConextMenu($event, config, termSelectionTextRef.value)
    );

    window.addEventListener('resize', () => handleResize(fitAddon, term));

    return new Promise(resolve => {
        resolve({
            termSelectionText: termSelectionTextRef.value
        });
    });
};

export { loadConfig, createTerminal };
