import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';

// 导入 Store
import { useTerminalStore } from '@/store/modules/terminal';

// 导入 hook
import { useDebounceFn, useWebSocket } from '@vueuse/core';

// 引入类型
import { ref, Ref } from 'vue';
import type { ILunaConfig } from '@/hooks/interface';

// 引入工具函数
import { defaultTheme } from '@/config';
import {
    generateWsURL,
    handleContextMenu,
    handleCustomKey,
    handleTerminalOnData,
    handleTerminalResize,
    handleTerminalSelection,
    onWebsocketOpen,
    onWebsocketWrong
} from './index';

import xtermTheme from 'xterm-theme';
import { createDiscreteApi } from 'naive-ui';
import { writeBufferToTerminal } from '@/utils';
import { formatMessage, sendEventToLuna, updateIcon, wsIsActivated } from '@/components/Terminal/helper';
import { storeToRefs } from 'pinia';
import { useParamsStore } from '@/store/modules/params.ts';
import { useLogger } from '@/hooks/useLogger.ts';
import { useSentry } from '@/hooks/useZsentry.ts';
import { Sentry } from 'nora-zmodemjs/src/zmodem_browser';

interface ITerminalReturn {
    sendWsMessage: (type: string, data: any) => void;
    setTerminalTheme: (themeName: string, terminal: Terminal, emit: any) => void;
    createTerminal: (el: HTMLElement, _type: string) => any;
}

interface ICallbackOptions {
    emitCallback?: (e: string, type: string, msg: any, terminal?: Terminal) => void;

    i18nCallBack?: (key: string) => string;
}

const { info } = useLogger('Terminal-hook');
const { message } = createDiscreteApi(['message']);

export const useTerminal = (callbackOptions: ICallbackOptions): ITerminalReturn => {
    let socket: WebSocket;
    let lunaConfig: ILunaConfig;

    let fitAddon: FitAddon;

    let type: string;
    let sentry: Sentry;

    let lunaId: Ref<string> = ref('');
    let origin: Ref<string> = ref('');
    let terminalId: Ref<string> = ref('');
    let lastSendTime: Ref<Date> = ref(new Date());
    let lastReceiveTime: Ref<Date> = ref(new Date());
    let termSelectionText: Ref<string> = ref('');
    let pingInterval: Ref<number | null> = ref(null);

    /**
     * 获取相关配置
     */
    const init = () => {
        fitAddon = new FitAddon();
        lunaConfig = useTerminalStore().getConfig;

        const debouncedFit = useDebounceFn(() => fitAddon.fit(), 500);

        window.addEventListener('resize', debouncedFit, false);
    };

    /**
     * 设置主题
     */
    const setTerminalTheme = (themeName: string, terminal: Terminal, emits: any) => {
        const theme = xtermTheme[themeName] || defaultTheme;

        terminal.options.theme = theme;

        emits('background-color', theme.background);
    };

    const sendDataFromWindow = (data: any) => {
        if (!wsIsActivated(socket)) return message.error('WebSocket Disconnected');

        const terminalStore = useTerminalStore();
        const { enableZmodem, zmodemStatus } = storeToRefs(terminalStore);

        if (enableZmodem.value && !zmodemStatus.value) {
            socket.send(formatMessage(terminalId.value, 'TERMINAL_DATA', data));
        }
    };

    const initCustomWindowEvent = (terminal: Terminal) => {
        window.addEventListener('message', (e: MessageEvent) => {
            const message = e.data;

            switch (message.name) {
                case 'PING': {
                    if (lunaId.value != null) return;

                    lunaId.value = message.id;
                    origin.value = e.origin;

                    sendEventToLuna('PONG', '', lunaId.value, origin.value);
                    break;
                }
                case 'CMD': {
                    sendDataFromWindow(message.data);
                    break;
                }
                case 'FOCUS': {
                    terminal.focus();
                    break;
                }
                case 'OPEN': {
                    callbackOptions.emitCallback && callbackOptions.emitCallback('event', 'open', '');
                    break;
                }
            }
        });

        window.SendTerminalData = data => {
            sendDataFromWindow(data);
        };

        window.Reconnect = () => {
            callbackOptions.emitCallback && callbackOptions.emitCallback('event', 'reconnect', '');
        };
    };

    /**
     * 设置相关请求信息
     *
     * @param type
     * @param data
     */
    const sendWsMessage = (type: string, data: any) => {
        socket.send(formatMessage(terminalId.value, type, JSON.stringify(data)));
    };

    /**
     * 初始化 El 节点相关事件
     *
     * @param {HTMLElement} el
     */
    const initElEvent = (el: HTMLElement) => {
        const onContextMenu = (e: MouseEvent) => {
            return handleContextMenu(e, lunaConfig, socket, termSelectionText.value);
        };

        el.addEventListener('mouseenter', () => fitAddon.fit(), false);
        el.addEventListener('contextmenu', onContextMenu, false);
    };

    /**
     * 初始化 Terminal 相关事件
     *
     * @param terminal
     */
    const initTerminalEvent = (terminal: Terminal) => {
        const debouncedTerminalResize = useDebounceFn(
            (cols: number, rows: number, type: string, terminalId: Ref<string>, socket: WebSocket) => {
                handleTerminalResize(cols, rows, type, terminalId.value, socket);
            },
            500
        );

        terminal.attachCustomKeyEventHandler((e: KeyboardEvent) => handleCustomKey(e, terminal));

        terminal.onSelectionChange(() => {
            return handleTerminalSelection(terminal, termSelectionText);
        });
        terminal.onData((data: string) => {
            lastSendTime.value = new Date();
            return handleTerminalOnData(data, type, terminalId.value, lunaConfig, socket);
        });
        terminal.onResize(({ cols, rows }) => {
            return debouncedTerminalResize(cols, rows, type, terminalId, socket);
        });
    };

    /**
     * message 分发
     *
     * @param socket
     * @param terminal
     * @param data
     */
    const dispatch = (socket: WebSocket, terminal: Terminal, data: string) => {
        if (data === undefined) return;

        let msg = JSON.parse(data);

        const paramsStore = useParamsStore();
        const terminalStore = useTerminalStore();

        const { enableZmodem, zmodemStatus } = storeToRefs(terminalStore);

        switch (msg.type) {
            case 'CONNECT': {
                terminalId.value = msg.id;

                const terminalData = {
                    cols: terminal && terminal.cols,
                    rows: terminal && terminal.rows,
                    code: paramsStore.shareCode
                };

                const info = JSON.parse(msg.data);

                paramsStore.setSetting(info.setting);
                paramsStore.setCurrentUser(info.user);

                updateIcon(info.setting);

                socket.send(formatMessage(terminalId.value, 'TERMINAL_INIT', JSON.stringify(terminalData)));
                break;
            }
            case 'CLOSE': {
                terminal.writeln('Receive Connection closed');
                socket.close();
                sendEventToLuna('CLOSE', '');
                break;
            }
            case 'PING':
                break;
            case 'TERMINAL_ACTION': {
                const action = msg.data;

                switch (action) {
                    case 'ZMODEM_START': {
                        terminalStore.setTerminalConfig('zmodemStatus', true);

                        if (enableZmodem.value) {
                            callbackOptions.i18nCallBack &&
                                message.info(callbackOptions.i18nCallBack('WaitFileTransfer'));
                        }
                        break;
                    }
                    case 'ZMODEM_END': {
                        if (!enableZmodem.value && zmodemStatus.value) {
                            callbackOptions.i18nCallBack &&
                                message.info(callbackOptions.i18nCallBack('EndFileTransfer'));

                            terminal.write('\r\n');

                            zmodemStatus.value = false;
                        }
                        break;
                    }
                    default: {
                        terminalStore.setTerminalConfig('zmodemStatus', false);
                    }
                }
                break;
            }
            case 'TERMINAL_ERROR':
            case 'ERROR': {
                message.error(msg.err);
                terminal.writeln(msg.err);
                break;
            }
            case 'MESSAGE_NOTIFY': {
                break;
            }
            default: {
                info(JSON.parse(data));
            }
        }

        callbackOptions.emitCallback && callbackOptions.emitCallback('socketData', msg.type, msg, terminal);
    };

    /**
     * 处理 onMessage
     *
     * @param socket
     * @param event
     * @param terminal
     */
    const handleMessage = (socket: WebSocket, event: MessageEvent, terminal: Terminal) => {
        lastReceiveTime.value = new Date();

        const terminalStore = useTerminalStore();
        const { enableZmodem, zmodemStatus } = storeToRefs(terminalStore);

        if (typeof event.data === 'object') {
            if (enableZmodem.value) {
                sentry.consume(event.data);
            } else {
                writeBufferToTerminal(enableZmodem.value, zmodemStatus.value, terminal, event.data);
            }
        } else {
            dispatch(socket, terminal, event.data);
        }
    };

    /**
     * 创建 Socket
     */
    const createWebSocket = (terminal: Terminal) => {
        const connectURL = generateWsURL();

        const { ws } = useWebSocket(connectURL, {
            protocols: ['JMS-KOKO'],
            onConnected: (socket: WebSocket) => {
                onWebsocketOpen(socket, lastSendTime.value, terminalId.value, pingInterval, lastReceiveTime);
            },
            onError: (_ws: WebSocket, event: Event) => {
                onWebsocketWrong(event, 'error', terminal);
            },
            onDisconnected: (_ws: WebSocket, event: CloseEvent) => {
                onWebsocketWrong(event, 'disconnected', terminal);
            },
            onMessage: (socket: WebSocket, event: MessageEvent) => {
                if (type === 'k8s') {
                    // handleK8sMessage();
                } else {
                    handleMessage(socket, event, terminal);
                }
            }
        });

        const { createSentry } = useSentry(lastSendTime, callbackOptions.i18nCallBack);

        socket = ws.value!;
        sentry = createSentry(ws.value!, terminal);

        return ws.value;
    };

    /**
     * 创建终端
     *
     * @param {HTMLElement} el 挂载节点
     * @param {string}_type type K8s 类型或者普通
     * @return Terminal
     */
    const createTerminal = (el: HTMLElement, _type: string) => {
        const { fontSize, lineHeight, fontFamily } = lunaConfig;

        const options = {
            fontSize,
            lineHeight,
            fontFamily,
            rightClickSelectsWord: true,
            theme: {
                background: '#1E1E1E'
            },
            scrollback: 5000
        };

        const terminal = new Terminal(options);

        terminal.loadAddon(fitAddon);
        terminal.open(el);
        terminal.focus();

        fitAddon.fit();

        type = _type;

        //* 初始化节点、Terminal 实例相关事件以及创建 Socket
        initElEvent(el);
        initTerminalEvent(terminal);

        const socket = createWebSocket(terminal);

        initCustomWindowEvent(terminal);

        return {
            socket,
            terminal
        };
    };

    init();

    return {
        sendWsMessage,
        createTerminal,
        setTerminalTheme
    };
};
