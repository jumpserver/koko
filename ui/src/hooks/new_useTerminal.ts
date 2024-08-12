import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { AttachAddon } from '@xterm/addon-attach';

// 导入 Store
import { useTerminalStore } from '@/store/modules/terminal';

// 导入 hook
import { useDebounceFn } from '@vueuse/core';

// 引入类型
import type { ILunaConfig } from '@/hooks/interface';

// 引入工具函数
import { nextTick } from 'vue';
import { handleContextMenu, handleTerminalResize } from './index';

interface ITerminalReturn {
    createTerminal: (el: HTMLElement, _socket: WebSocket, _type: string) => Promise<Terminal>;
    setTerminalTheme: () => void;
}

export const useTerminal = (): ITerminalReturn => {
    let socket: WebSocket;
    let lunaConfig: ILunaConfig;

    let fitAddon: FitAddon;
    let attachAddon: AttachAddon;

    let type: string;
    let terminalId: string;
    let termSelectionText: string;

    /**
     * 获取相关配置
     */
    const init = () => {
        fitAddon = new FitAddon();
        attachAddon = new AttachAddon(socket);
        lunaConfig = useTerminalStore().getConfig;

        const debouncedFit = useDebounceFn(() => fitAddon.fit(), 500);

        window.addEventListener('resize', debouncedFit, false);
    };

    /**
     * 设置主题
     */
    const setTerminalTheme = () => {};

    /**
     * 初始化 El 节点相关事件
     *
     * @param {HTMLElement} el
     */
    const initElEvent = (el: HTMLElement) => {
        const onContextMenu = (e: MouseEvent) => handleContextMenu(e, lunaConfig, socket, termSelectionText);

        el.addEventListener('mouseenter', () => fitAddon.fit(), false);
        el.addEventListener('contextmenu', onContextMenu, false);
    };

    const initTerminalEvent = (terminal: Terminal) => {
        const debouncedTerminalResize = useDebounceFn(
            (cols: number, rows: number, type: string, terminalId: string, socket: WebSocket) => {
                handleTerminalResize(cols, rows, type, terminalId, socket);
            },
            500
        );

        terminal.onResize(({ cols, rows }) => debouncedTerminalResize(cols, rows, type, terminalId, socket));
    };

    /**
     * 创建终端
     *
     * @param {HTMLElement} el 挂载节点
     * @param {string}_type type K8s 类型或者普通
     * @param {WebSocket} _socket WebSocket 实例
     * @return Terminal
     */
    const createTerminal = async (el: HTMLElement, _socket: WebSocket, _type: string): Promise<Terminal> => {
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
        socket = _socket;

        // 初始化节点与 Terminal 实例相关事件
        await nextTick(() => {
            initElEvent(el);
            initTerminalEvent(terminal);
        });

        return terminal;
    };

    init();

    return {
        createTerminal,
        setTerminalTheme
    };
};
