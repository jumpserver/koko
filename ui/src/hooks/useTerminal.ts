import { Ref, ref } from 'vue';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { useLogger } from '@/hooks/useLogger.ts';
import { AsciiBackspace, AsciiCtrlC, AsciiCtrlZ, AsciiDel, defaultTheme } from '@/config';
import { formatMessage, sendEventToLuna, wsIsActivated } from '@/components/Terminal/helper';

import type { ILunaConfig } from './interface';

import xtermTheme from 'xterm-theme';

const { debug } = useLogger('Terminal-Hook');

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
      text = await navigator.clipboard.readText();
    } catch {
      if (termSelectionText.value !== '') {
        text = termSelectionText.value;
      }
    }

    e.preventDefault();

    if (wsIsActivated(ws)) {
      ws.send(formatMessage('1', 'TERMINAL_DATA', text));
    }

    return text;
  };

  // 在不支持 clipboard 时的降级方案
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

  // 获取当前终端中的选定文本
  const handleSelection = async (terminal: Terminal) => {
    // todo)) 现在会有问题
    debug('Select Change');

    termSelectionText.value = terminal.getSelection().trim();

    if (!navigator.clipboard) return fallbackCopyTextToClipboard(termSelectionText.value);

    try {
      await navigator.clipboard.writeText(termSelectionText.value);
    } catch (e) {
      fallbackCopyTextToClipboard(termSelectionText.value);
    }
  };

  // 对用户设置的特定键映射配置
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

  // 处理 Terminal 的 onData 事件
  const handleTerminalOnData = (data: any, config: ILunaConfig, ws: WebSocket) => {
    if (!wsIsActivated(ws)) return debug('WebSocket Closed');

    if (!enableZmodem && zmodemStatus?.value) {
      return debug('未开启 Zmodem 且当前在 Zmodem 状态，不允许输入');
    }

    debug('Term on data event');
    data = preprocessInput(data, config);

    console.log('data', data);

    lastSendTime.value = new Date();

    const eventType = type === 'common' ? 'TERMINAL_DATA' : 'TERMINAL_K8S_DATA';

    if (type === 'common') {
      sendEventToLuna('KEYBOARDEVENT', '');
    } else {
      // k8s 的情况, data 需要额外处理
      // todo))
      data = {
        k8s_id: '',
        namespace: '',
        pod: '',
        container: '',
        ...data
      };
    }

    ws.send(formatMessage(<string>id?.value, eventType, data));
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
  const initTerminalEvent = (
    ws: WebSocket,
    el: HTMLElement,
    terminal: Terminal,
    config: ILunaConfig
  ) => {
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
