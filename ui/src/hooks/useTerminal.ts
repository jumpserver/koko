import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { useLogger } from '@/hooks/useLogger.ts';
import { sendEventToLuna } from '@/components/Terminal/helper';
import { AsciiBackspace, AsciiCtrlC, AsciiCtrlZ, AsciiDel, defaultTheme } from '@/config';

import type { ILunaConfig } from './interface';

import xtermTheme from 'xterm-theme';
import ZmodemBrowser, { SentryConfig } from 'nora-zmodemjs/src/zmodem_browser';

const { debug } = useLogger('Terminal-Hook');

export const useTerminal = () => {
  let term: Terminal;
  let fitAddon: FitAddon;
  let termSelectionText: string;
  let config: ILunaConfig = {};

  const createZsentry = (config: SentryConfig) => {
    return new ZmodemBrowser.Sentry(config);
  };

  const setTerminalTheme = (
    themeName: string,
    emits?: (event: 'background-color', backgroundColor: string) => void
  ) => {
    const theme = xtermTheme[themeName] || defaultTheme;

    term.options.theme = theme;

    debug(`Theme: ${themeName}`);

    emits && emits('background-color', theme.background);
  };

  /**
   * @description 获取 Luna 配置
   */
  const getLunaConfig = (): ILunaConfig => {
    let fontSize: number = 14;
    let quickPaste: string = '0';
    let backspaceAsCtrlH: string = '0';
    let localSettings: string | null = localStorage.getItem('LunaSetting');

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

    // 根据用户的操作系统类型设置行高
    const ua: string = navigator.userAgent.toLowerCase();
    config['lineHeight'] = ua.indexOf('windows') !== -1 ? 1.2 : 1;

    return config;
  };

  /**
   * @description 自适应 Terminal 大小
   */
  const handleResize = (): void => {
    fitAddon.fit();
    debug(`Windows resize event, ${term.cols}, ${term.rows}, ${term}`);
  };

  /**
   * @description 用于附加自定义的键盘事件处理程序,允许开发者拦截和处理终端中的键盘事件
   */
  const handleCustomKeyEvent = (e: KeyboardEvent, lunaId: string, origin: string) => {
    if (e.altKey && (e.key === 'ArrowRight' || e.key === 'ArrowLeft')) {
      switch (e.key) {
        case 'ArrowRight':
          sendEventToLuna('KEYEVENT', 'alt+right', lunaId, origin);
          break;
        case 'ArrowLeft':
          sendEventToLuna('KEYEVENT', 'alt+left', lunaId, origin);
          break;
      }
    }

    if (e.ctrlKey && e.key === 'c' && term.hasSelection()) {
      return false;
    }

    return !(e.ctrlKey && e.key === 'v');
  };

  /**
   * @description 处理右键菜单事件
   */
  const handleConextMenu = async (e: MouseEvent) => {
    if (e.ctrlKey || config.quickPaste !== '1') return;

    let text: string = '';

    try {
      text = await navigator.clipboard.readText();
    } catch {
      if (termSelectionText !== '') {
        text = termSelectionText;
      }
    }

    e.preventDefault();

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
  const getSelection = async () => {
    debug('Select Change');

    termSelectionText = term.getSelection().trim();

    if (!navigator.clipboard) return fallbackCopyTextToClipboard(termSelectionText);

    try {
      await navigator.clipboard.writeText(termSelectionText);
    } catch (e) {
      fallbackCopyTextToClipboard(termSelectionText);
    }
  };

  /**
   * @description 聚焦 Terminal
   */
  const terminalFocus = (): void => {
    term.focus();
  };

  /**
   * @description 对用户设置的特定键映射配置
   * @param data
   */
  const preprocessInput = (data: string) => {
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
   * @description 创建 Terminal
   * @param {HTMLElement} el
   * @param {ILunaConfig} config
   */
  const createTerminal = (el: HTMLElement, config: ILunaConfig) => {
    term = new Terminal({
      fontSize: config.fontSize,
      lineHeight: config.lineHeight,
      fontFamily: 'monaco, Consolas, "Lucida Console", monospace',
      rightClickSelectsWord: true,
      theme: {
        background: '#1E1E1E'
      },
      scrollback: 5000
    });
    fitAddon = new FitAddon();

    term.loadAddon(fitAddon);
    term.open(el);
    fitAddon.fit();
    term.focus();

    window.addEventListener('resize', handleResize, false);
    term.onSelectionChange(() => getSelection());
    el.addEventListener('mouseenter', () => terminalFocus(), false);

    return {
      term,
      fitAddon
    };
  };

  return {
    getLunaConfig,
    createZsentry,
    createTerminal,
    preprocessInput,
    handleConextMenu,
    setTerminalTheme,
    handleCustomKeyEvent
  };
};
