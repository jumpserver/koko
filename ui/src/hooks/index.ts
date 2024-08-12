import type { ILunaConfig } from '@/hooks/interface';
import { formatMessage } from '@/components/Terminal/helper';
import { useLogger } from '@/hooks/useLogger.ts';

const { info } = useLogger('Hook Helper');

/**
 * 右键复制文本
 *
 * @param e
 * @param config
 * @param socket
 * @param termSelectionText
 */
export const handleContextMenu = async (
    e: MouseEvent,
    config: ILunaConfig,
    socket: WebSocket,
    termSelectionText: string
) => {
    if (e.ctrlKey || config.quickPaste !== '1') return;

    let text: string = '';

    try {
        text = await navigator.clipboard.readText();
    } catch (e) {
        if (termSelectionText !== '') text = termSelectionText;
    }

    e.preventDefault();

    socket.send(formatMessage('1', 'TERMINAL_DATA', text));
};

/**
 * Terminal Resize 事件处理
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

    const eventType = type === 'k8s' ? 'TERMINAL_K8S_RESIZE' : 'TERMINAL_RESIZE';
    const resizeData = JSON.stringify({ cols, rows });

    if (type === 'k8s') {
        data = {
            k8s_id: '',
            namespace: '',
            pod: '',
            container: '',
            resizeData
        };
    }

    socket.send(formatMessage(terminalId, eventType, data));
};
