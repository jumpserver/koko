import { Ref, ref } from 'vue';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { useLogger } from '@/hooks/useLogger.ts';
import { defaultTheme } from '@/config';
import { formatMessage, sendEventToLuna, wsIsActivated } from '@/components/Terminal/helper';
import * as clipboard from 'clipboard-polyfill';
import type { ILunaConfig } from './interface';

import { readText } from 'clipboard-polyfill';

import xtermTheme from 'xterm-theme';
import { useTreeStore } from '@/store/modules/tree.ts';
import { storeToRefs } from 'pinia';
import { preprocessInput } from '@/utils';
import { createDiscreteApi } from 'naive-ui';

const { debug } = useLogger('Terminal-Hook');
const { message } = createDiscreteApi(['message']);
export const useTerminal = (
    id: Ref<string>,
    type: string,
    zmodemStatus?: Ref<boolean>,
    enableZmodem?: boolean,
    lastSendTime: Ref<Date> = ref(new Date()),
    emits?: (event: 'background-color', backgroundColor: string) => void
) => {
    let termSelectionText = ref<string>('');

    // 设置 Terminal 主题
    const setTerminalTheme = (themeName: string, term: Terminal) => {
        const theme = xtermTheme[themeName] || defaultTheme;

        term.options.theme = theme;

        debug(`Theme: ${themeName}`);

        emits && emits('background-color', theme.background);
    };

    // 用于附加自定义的键盘事件处理程序,允许开发者拦截和处理终端中的键盘事件
    const handleKeyEvent = (e: KeyboardEvent, terminal: Terminal) => {
        if (e.altKey && (e.key === 'ArrowRight' || e.key === 'ArrowLeft')) {
            switch (e.key) {
                case 'ArrowRight':
                    sendEventToLuna('KEYEVENT', 'alt+right');
                    break;
                case 'ArrowLeft':
                    sendEventToLuna('KEYEVENT', 'alt+left');
                    break;
            }
        }

        if (e.ctrlKey && e.key === 'c' && terminal.hasSelection()) {
            return false;
        }

        return !(e.ctrlKey && e.key === 'v');
    };

    // 处理右键菜单事件
    const handleContextMenu = async (e: MouseEvent, config: ILunaConfig, ws: WebSocket) => {
        if (e.ctrlKey || config.quickPaste !== '1') return;

        let text: string = '';

        try {
            text = await readText();
            console.log('剪贴板内容：', text);
        } catch (err) {
            if (termSelectionText.value !== '') text = termSelectionText.value;
            message.info(`${err}`);
        }
        ws.send(formatMessage('1', 'TERMINAL_DATA', text));
        e.preventDefault();

        return text;
    };

    // 获取当前终端中的选定文本
    const handleSelection = async (terminal: Terminal) => {
        debug('Select Change');

        termSelectionText.value = terminal.getSelection().trim();

        clipboard
            .writeText(termSelectionText.value)
            .then(() => {
                message.success('Copied!');
            })
            .catch(e => {
                message.error(`Copy Error for ${e}`);
            });
    };

    // 处理 Terminal 的 onData 事件
    const handleTerminalOnData = (data: any, config: ILunaConfig, ws: WebSocket) => {
        if (!wsIsActivated(ws)) return debug('WebSocket Closed');

        if (!enableZmodem && zmodemStatus?.value) {
            return debug('未开启 Zmodem 且当前在 Zmodem 状态，不允许输入');
        }

        debug('Term on data event');
        data = preprocessInput(data, config);

        lastSendTime.value = new Date();

        const eventType = type === 'common' ? 'TERMINAL_DATA' : 'TERMINAL_K8S_DATA';

        if (type === 'common') {
            sendEventToLuna('KEYBOARDEVENT', '');
            ws.send(formatMessage(<string>id?.value, eventType, data));
        } else {
            const treeStore = useTreeStore();

            const { currentNode } = storeToRefs(treeStore);

            if (currentNode.value.children) {
                const currentItem = currentNode.value.children[0];

                data = {
                    k8s_id: currentItem.k8s_id,
                    namespace: currentItem.namespace,
                    pod: currentItem.pod,
                    container: currentItem.container,
                    type: eventType,
                    id: id.value,
                    ...data
                };

                ws.send(JSON.stringify(data));
            }
        }
    };

    // 处理 Terminal 的 resize 事件
    const handleTerminalOnResize = (ws: WebSocket, cols: any, rows: any) => {
        if (!wsIsActivated(ws)) return;

        debug('Send Term Resize');

        const eventType = type === 'common' ? 'TERMINAL_RESIZE' : 'TERMINAL_K8S_RESIZE';
        let data = null;
        let resizeData = null;

        if (type === 'k8s') {
            resizeData = JSON.stringify({ cols, rows });

            // todo))
            data = {
                k8s_id: '',
                namespace: '',
                pod: '',
                container: '',
                resizeData
            };
        } else {
            data = JSON.stringify({ cols, rows });
        }

        ws.send(formatMessage(<string>id?.value, eventType, data));
    };

    // 初始化 el 与 Terminal 相关事件
    const initTerminalEvent = (ws: WebSocket, el: HTMLElement, terminal: Terminal, config: ILunaConfig) => {
        terminal.onSelectionChange(() => handleSelection(terminal));
        terminal.onData(data => {
            handleTerminalOnData(data, config, ws);
        });
        terminal.onResize(({ cols, rows }) => handleTerminalOnResize(ws, cols, rows));
        terminal.attachCustomKeyEventHandler(e => handleKeyEvent(e, terminal));

        el.addEventListener('mouseenter', () => terminal.focus(), false);
        el.addEventListener('contextmenu', (e: MouseEvent) => handleContextMenu(e, config, ws));
    };

    // 创建 Terminal
    const createTerminal = (el: HTMLElement, config: ILunaConfig) => {
        const terminal = new Terminal({
            fontSize: config.fontSize,
            lineHeight: config.lineHeight,
            fontFamily: 'monaco, Consolas, "Lucida Console", monospace',
            rightClickSelectsWord: true,
            theme: {
                background: '#1E1E1E'
            },
            scrollback: 5000
        });

        const fitAddon: FitAddon = new FitAddon();

        terminal.loadAddon(fitAddon);
        terminal.open(el);
        fitAddon.fit();
        terminal.focus();

        window.addEventListener(
            'resize',
            () => {
                fitAddon.fit();
                debug(`Windows resize event, ${terminal.cols}, ${terminal.rows}, ${terminal}`);
            },
            false
        );

        return {
            terminal,
            fitAddon
        };
    };

    return {
        initTerminalEvent,
        createTerminal,
        setTerminalTheme,
        handleTerminalOnData
    };
};
