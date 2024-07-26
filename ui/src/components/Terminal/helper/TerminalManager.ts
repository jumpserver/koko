import { Ref } from 'vue';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { useLogger } from '@/hooks/useLogger';
import { ILunaConfig } from '@/components/interface';
import { AsciiBackspace, AsciiCtrlC, AsciiCtrlZ, AsciiDel } from '@/config';
import { sendEventToLuna } from '@/components/Terminal/helper/index.ts';

const { debug, setLogLevel } = useLogger('TerminalManager');
setLogLevel('DEBUG');

class TerminalManager {
  public term: Terminal | null = null;
  private readonly config: ILunaConfig;

  constructor(el: HTMLElement) {
    this.config = this.loadConfig();
    this.term = this.createTerminal(el);
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
    const config: ILunaConfig = this.loadLunaConfig();
    const ua: string = navigator.userAgent.toLowerCase();

    config['lineHeight'] = ua.indexOf('windows') !== -1 ? 1.2 : 1;

    return config;
  }

  /**
   * @description 处理视口大小变化
   * @param {FitAddon} fitAddon
   * @param {Terminal} term
   */
  public handleResize(fitAddon: FitAddon, term: Terminal) {
    fitAddon.fit();
    debug(`Windows resize event, ${term.cols}, ${term.rows}, ${term}`);
  }

  /**
   * @description 处理鼠标移入
   */
  public handleMouseenter(term: Terminal) {
    term.focus();
  }

  /**
   * @description 处理 Terminal 实例 changeSelect 事件
   * @param term
   * @param termSelectionTextRef
   */
  public handleSelectionChange(term: Terminal, termSelectionTextRef: Ref<string>) {
    const termSelectionText: string = term.getSelection().trim();
    debug('select change');
    navigator.clipboard.writeText(termSelectionText).then();
    termSelectionTextRef.value = termSelectionText;
  }

  /**
   * @description 处理 Terminal 中键盘事件
   * @param {KeyboardEvent} e
   * @param {Terminal} term
   * @param {string} lunaId
   * @param {origin} origin
   */
  public handleCustomKeyEvent(e: KeyboardEvent, term: Terminal, lunaId: string, origin: string) {
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
  }

  /**
   * @description 处理右键菜单事件
   * @param e 鼠标事件
   * @param termSelectionText
   */
  public async handleConextMenu(e: MouseEvent, termSelectionText: string) {
    if (e.ctrlKey || this.config.quickPaste !== '1') return;

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
  }

  /**
   * @description 创建 Terminal
   * @param {HTMLElement} el
   */
  public createTerminal(el: HTMLElement) {
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

    return term;
  }

  /**
   * @description 对用户设置的特定键映射配置
   * @param data
   */
  public preprocessInput(data: string) {
    /* 如果配置项 backspaceAsCtrlH 启用（值为 "1"），并且输入数据包含删除键的 ASCII 码 (AsciiDel，即 127)，
       它会将其替换为退格键的 ASCII 码 (AsciiBackspace，即 8) */
    if (this.config.backspaceAsCtrlH === '1') {
      if (data.charCodeAt(0) === AsciiDel) {
        data = String.fromCharCode(AsciiBackspace);
        debug('backspaceAsCtrlH enabled');
      }
    }

    /* 如果配置项 ctrlCAsCtrlZ 启用（值为 "1"），并且输入数据包含 Ctrl+C 的 ASCII 码 (AsciiCtrlC，即 3)，
       它会将其替换为 Ctrl+Z 的 ASCII 码 (AsciiCtrlZ，即 26)。 */
    if (this.config.ctrlCAsCtrlZ === '1') {
      if (data.charCodeAt(0) === AsciiCtrlC) {
        data = String.fromCharCode(AsciiCtrlZ);
        debug('ctrlCAsCtrlZ enabled');
      }
    }
    return data;
  }
}

export default TerminalManager;
