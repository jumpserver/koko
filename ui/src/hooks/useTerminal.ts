import { Ref, ref } from 'vue';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { useLogger } from '@/hooks/useLogger.ts';
import { AsciiBackspace, AsciiCtrlC, AsciiCtrlZ, AsciiDel, defaultTheme } from '@/config';
import { formatMessage, sendEventToLuna, wsIsActivated } from '@/components/Terminal/helper';
import type { ILunaConfig } from './interface';

import xtermTheme from 'xterm-theme';

const { debug } = useLogger('Terminal-Hook');

export const useTerminal = () => {
  let terminal: Terminal;
  let termSelectionText = ref<string>('');

  const setTerminalTheme = (
    themeName: string,
    term: Terminal,
    emits?: (event: 'background-color', backgroundColor: string) => void
  ) => {
    const theme = xtermTheme[themeName] || defaultTheme;

    term.options.theme = theme;

    debug(`Theme: ${themeName}`);

    emits && emits('background-color', theme.background);
  };

  /**
   * @description 用于附加自定义的键盘事件处理程序,允许开发者拦截和处理终端中的键盘事件
   */
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

  /**
   * @description 处理右键菜单事件
   * @param {MouseEvent} e 鼠标事件
   * @param {ILunaConfig} config Luna 配置
   * @param {WebSocket} ws
   */
  const handleContextMenu = async (e: MouseEvent, config: ILunaConfig, ws: WebSocket) => {
    if (e.ctrlKey || config.quickPaste !== '1') return;

    let text: string = '';

    try {
      text = await navigator.clipboard.readText();
    } catch {
      if (termSelectionText.value !== '') {
        text = termSelectionText.value;
      }
    }

    e.preventDefault();

    //todo))
    if (wsIsActivated(ws)) {
      ws.send(formatMessage('1', 'TERMINAL_DATA', text));
    }

    return text;
  };

  /**
   * @description 在不支持 clipboard 时的降级方案
   * @param text
   */
  const fallbackCopyTextToClipboard = (text: string): void => {
    const textArea = document.createElement('textarea');
    textArea.value = text;

    // Avoid scrolling to bottom
    textArea.style.position = 'fixed';
    textArea.style.top = '0';
    textArea.style.left = '0';
    textArea.style.width = '2em';
    textArea.style.height = '2em';
    textArea.style.padding = '0';
    textArea.style.border = 'none';
    textArea.style.outline = 'none';
    textArea.style.boxShadow = 'none';
    textArea.style.background = 'transparent';

    document.body.appendChild(textArea);
    textArea.focus();
    textArea.select();

    try {
      const successful = document.execCommand('copy');
      const msg = successful ? 'successful' : 'unsuccessful';
      debug('Fallback: Copying text command was ' + msg);
    } catch (err) {
      console.error('Fallback: Oops, unable to copy', err);
    }

    document.body.removeChild(textArea);
  };

  /**
   * @description 获取当前终端中的选定文本  handleSelectionChange
   */
  const handleSelection = async (terminal: Terminal) => {
    debug('Select Change');

    termSelectionText.value = terminal.getSelection().trim();

    if (!navigator.clipboard) return fallbackCopyTextToClipboard(termSelectionText.value);

    try {
      await navigator.clipboard.writeText(termSelectionText.value);
    } catch (e) {
      fallbackCopyTextToClipboard(termSelectionText.value);
    }
  };

  /**
   * @description 对用户设置的特定键映射配置
   * @param data
   */
  const preprocessInput = (data: string, config: ILunaConfig) => {
    // 如果配置项 backspaceAsCtrlH 启用（值为 "1"），并且输入数据包含删除键的 ASCII 码 (AsciiDel，即 127)，
    // 它会将其替换为退格键的 ASCII 码 (AsciiBackspace，即 8)
    if (config.backspaceAsCtrlH === '1') {
      if (data.charCodeAt(0) === AsciiDel) {
        data = String.fromCharCode(AsciiBackspace);
        debug('backspaceAsCtrlH enabled');
      }
    }

    // 如果配置项 ctrlCAsCtrlZ 启用（值为 "1"），并且输入数据包含 Ctrl+C 的 ASCII 码 (AsciiCtrlC，即 3)，
    // 它会将其替换为 Ctrl+Z 的 ASCII 码 (AsciiCtrlZ，即 26)。
    if (config.ctrlCAsCtrlZ === '1') {
      if (data.charCodeAt(0) === AsciiCtrlC) {
        data = String.fromCharCode(AsciiCtrlZ);
        debug('ctrlCAsCtrlZ enabled');
      }
    }
    return data;
  };

  /**
   * @description 处理 Terminal 的 onData 事件
   * @param ws
   * @param data
   * @param terminalId
   * @param config
   * @param enableZmodem
   * @param zmodemStatus
   * @param lastSendTime
   */
  const handleTerminalOnData = (
    ws: WebSocket,
    data: any,
    terminalId: string,
    config: ILunaConfig,
    enableZmodem: boolean,
    zmodemStatus: boolean,
    lastSendTime: Ref<Date>
  ) => {
    console.log('enter');
    if (!wsIsActivated(ws)) return debug('WebSocket Closed');

    if (!enableZmodem && zmodemStatus) {
      return debug('未开启 Zmodem 且当前在 Zmodem 状态，不允许输入');
    }

    lastSendTime.value = new Date();

    debug('Term on data event');

    data = preprocessInput(data, config);

    sendEventToLuna('KEYBOARDEVENT', '');

    ws.send(formatMessage(terminalId, 'TERMINAL_DATA', data));
  };

  /**
   * @description 处理 Terminal 的 resize 事件
   * @param ws
   * @param cols
   * @param rows
   * @param terminalId
   */
  const handleTerminalOnResize = (ws: WebSocket, cols: any, rows: any, terminalId: string) => {
    if (!wsIsActivated(ws)) return;

    debug('Send Term Resize');

    ws.send(formatMessage(terminalId, 'TERMINAL_RESIZE', JSON.stringify({ cols, rows })));
  };

  /**
   * @description 初始化 el 与 Terminal 相关事件
   * @param el
   * @param terminal
   * @param config
   * @param ws
   * @param terminalId
   * @param enableZmodem
   * @param zmodemStatus
   * @param lastSendTime
   */
  const initTerminalEvent = (
    ws: WebSocket,
    el: HTMLElement,
    terminal: Terminal,
    config: ILunaConfig,
    terminalId: string,
    enableZmodem: boolean,
    zmodemStatus: boolean,
    lastSendTime: Ref<Date>
  ) => {
    terminal.onSelectionChange(() => handleSelection(terminal));
    terminal.attachCustomKeyEventHandler(e => handleKeyEvent(e, terminal));
    terminal.onResize(({ cols, rows }) => handleTerminalOnResize(ws, cols, rows, terminalId));
    terminal.onData(data =>
      handleTerminalOnData(ws, data, terminalId, config, enableZmodem, zmodemStatus, lastSendTime)
    );

    el.addEventListener('mouseenter', () => terminal.focus(), false);
    el.addEventListener('contextmenu', (e: MouseEvent) => handleContextMenu(e, config, ws));
  };

  /**
   * @description 创建 Terminal
   * @param {HTMLElement} el
   * @param {ILunaConfig} config
   */
  const createTerminal = (el: HTMLElement, config: ILunaConfig) => {
    terminal = new Terminal({
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
    handleTerminalOnData,
    handleTerminalOnResize
  };
};
