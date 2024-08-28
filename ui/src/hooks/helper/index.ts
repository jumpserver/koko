import { Ref } from 'vue';
import type { ILunaConfig } from '@/hooks/interface';

import { Terminal } from '@xterm/xterm';
import { useLogger } from '@/hooks/useLogger.ts';
import { formatMessage, handleError, sendEventToLuna } from '@/components/CustomTerminal/helper';

// 引入 Store
import { useTreeStore } from '@/store/modules/tree.ts';
import { useTerminalStore } from '@/store/modules/terminal.ts';

// 引入 API
import { storeToRefs } from 'pinia';
import { useRoute } from 'vue-router';
import { createDiscreteApi } from 'naive-ui';
import { readText } from 'clipboard-polyfill';
import { fireEvent, preprocessInput } from '@/utils';

import * as clipboard from 'clipboard-polyfill';
import { BASE_WS_URL, MaxTimeout } from '@/config';

const { info } = useLogger('Hook Helper');
const { message } = createDiscreteApi(['message']);

/**
 * 右键复制文本
 *
 * @param e
 * @param config
 * @param socket
 * @param terminalId
 * @param termSelectionText
 * @param k8s_id
 */
export const handleContextMenu = async (
    e: MouseEvent,
    config: ILunaConfig,
    socket: WebSocket,
    terminalId: string,
    termSelectionText: string,
    k8s_id: string | undefined
) => {
    e.preventDefault();
    if (e.ctrlKey || config.quickPaste !== '1') return;

    let text: string = '';

    try {
        text = await readText();
    } catch (e) {
        if (termSelectionText !== '') text = termSelectionText;
    }

    if (k8s_id) {
        socket.send(
            JSON.stringify({
                id: terminalId,
                k8s_id,
                type: 'TERMINAL_K8S_DATA',
                data: text
            })
        );
    } else {
        socket.send(formatMessage(terminalId, 'TERMINAL_DATA', text));
    }
};

/**
 * CustomTerminal Resize 事件处理
 *
 * @param cols
 * @param rows
 * @param type
 * @param terminalId
 * @param socket
 */
export const handleTerminalResize = (
    cols: number,
    rows: number,
    type: string,
    terminalId: string,
    socket: WebSocket
) => {
    let data;

    info('Send Term Resize');

    const treeStore = useTreeStore();
    const { currentNode } = storeToRefs(treeStore);

    const eventType = type === 'k8s' ? 'TERMINAL_K8S_RESIZE' : 'TERMINAL_RESIZE';
    const resizeData = JSON.stringify({ cols, rows });

    if (type === 'k8s' && currentNode.value.children) {
        const currentItem = currentNode.value.children[0];

        data = {
            k8s_id: currentItem.k8s_id,
            namespace: currentItem.namespace,
            pod: currentItem.pod,
            container: currentItem.container,
            type: eventType,
            id: terminalId,
            resizeData
        };
    }

    socket.send(formatMessage(terminalId, eventType, data));
};

/**
 * 针对特定的键盘组合进行操作
 *
 * @param e
 * @param terminal
 */
export const handleCustomKey = (e: KeyboardEvent, terminal: Terminal): boolean => {
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

/**
 *左键选中
 *
 * @param terminal
 * @param termSelectionText
 */
export const handleTerminalSelection = async (terminal: Terminal, termSelectionText: Ref<string>) => {
    termSelectionText.value = terminal.getSelection().trim();

    if (termSelectionText.value !== '') {
        clipboard
            .writeText(termSelectionText.value)
            .then(() => {})
            .catch(e => {
                message.error(`Copy Error for ${e}`);
            });
    } else {
        // message.warning('Please select the text before copying');
    }
};

/**
 * 处理 CustomTerminal 的输入事件
 *
 * @param data
 * @param type
 * @param terminalId
 * @param config
 * @param socket
 */
export const handleTerminalOnData = (
    data: string,
    type: string,
    terminalId: string,
    config: ILunaConfig,
    socket: WebSocket
) => {
    const terminalStore = useTerminalStore();

    const { enableZmodem, zmodemStatus } = storeToRefs(terminalStore);

    if (!enableZmodem.value && zmodemStatus.value) {
        return message.warning('未开启 Zmodem 且当前在 Zmodem 状态，不允许输入');
    }

    data = preprocessInput(data, config);

    const eventType = type === 'k8s' ? 'TERMINAL_K8S_DATA' : 'TERMINAL_DATA';

    if (type === 'k8s') {
        const treeStore = useTreeStore();
        const { currentNode } = storeToRefs(treeStore);

        if (currentNode.value.children) {
            const currentItem = currentNode.value.children[0];

            return socket.send(
                JSON.stringify({
                    data: data,
                    id: terminalId,
                    type: eventType,
                    pod: currentItem.pod,
                    k8s_id: currentItem.k8s_id,
                    namespace: currentItem.namespace,
                    container: currentItem.container
                })
            );
        } else {
            return socket.send(
                JSON.stringify({
                    data: data,
                    id: terminalId,
                    type: eventType,
                    pod: '',
                    k8s_id: currentNode.value.k8s_id,
                    namespace: '',
                    container: ''
                })
            );
        }
    }

    sendEventToLuna('KEYBOARDEVENT', '');

    socket.send(formatMessage(terminalId, eventType, data));
};

/**
 * Socket 打开时的回调
 *
 * @param socket
 * @param lastSendTime
 * @param pingInterval
 * @param lastReceiveTime
 * @param terminalId
 */
export const onWebsocketOpen = (
    socket: WebSocket,
    lastSendTime: Date,
    terminalId: string,
    pingInterval: Ref<number | null>,
    lastReceiveTime: Ref<Date>
) => {
    socket.binaryType = 'arraybuffer';
    sendEventToLuna('CONNECTED', '');

    if (pingInterval.value) clearInterval(pingInterval.value);

    pingInterval.value = setInterval(() => {
        if (socket.CLOSED === socket.readyState || socket.CLOSING === socket.readyState) {
            return clearInterval(pingInterval.value!);
        }

        let currentDate: Date = new Date();

        if (lastReceiveTime.value.getTime() - currentDate.getTime() > MaxTimeout) {
            message.info('More than 30s do not receive data');
        }

        let pingTimeout: number = currentDate.getTime() - lastSendTime.getTime();

        if (pingTimeout < 0) return;

        socket.send(formatMessage(terminalId, 'PING', ''));
    }, 25 * 1000);
};

/**
 * 生成 Socket url
 */
export const generateWsURL = () => {
    const route = useRoute();

    const routeName = route.name;
    const urlParams = new URLSearchParams(window.location.search.slice(1));

    let connectURL;

    switch (routeName) {
        case 'Token': {
            const params = route.params;
            const requireParams = new URLSearchParams();

            requireParams.append('type', 'token');
            requireParams.append('target_id', params.id ? params.id.toString() : '');

            connectURL = BASE_WS_URL + '/koko/ws/token/?' + requireParams.toString();
            break;
        }
        case 'TokenParams': {
            connectURL = urlParams ? `${BASE_WS_URL}/koko/ws/token/?${urlParams.toString()}` : '';
            break;
        }
        case 'kubernetes': {
            connectURL = `${BASE_WS_URL}/koko/ws/terminal/?token=${route.query.token}&type=k8s`;
            break;
        }
        case 'Share': {
            const id = route.params.id as string;
            const requireParams = new URLSearchParams();

            requireParams.append('type', 'share');
            requireParams.append('target_id', id);

            connectURL = BASE_WS_URL + '/koko/ws/terminal/?' + requireParams.toString();
            break;
        }
        default: {
            connectURL = urlParams ? `${BASE_WS_URL}/koko/ws/terminal/?${urlParams.toString()}` : '';
        }
    }

    if (!connectURL) {
        message.error('Unable to generate WebSocket URL, missing parameters.');
    }

    return connectURL;
};

/**
 * Socket 出错或断开连接的回调
 *
 * @param event
 * @param terminal
 * @param type
 */
export const onWebsocketWrong = (event: Event, type: string, terminal?: Terminal) => {
    switch (type) {
        case 'error': {
            terminal ? terminal.write('Connection Websocket Error') : '';
            break;
        }
        case 'disconnected': {
            terminal ? terminal.write('Connection Websocket Closed') : '';
            break;
        }
    }

    fireEvent(new Event('CLOSE', {}));
    handleError(event);
};

/**
 * @description 将 Base64 转化为字节数组
 * @param base64
 */
export const base64ToUint8Array = (base64: string): Uint8Array => {
    // 转为原始的二进制字符串（binaryString）。
    const binaryString = atob(base64);
    const len = binaryString.length;

    const bytes = new Uint8Array(len);
    for (let i = 0; i < len; i++) {
        bytes[i] = binaryString.charCodeAt(i);
    }
    return bytes;
};
