import xtermTheme from 'xterm-theme';

import { storeToRefs } from 'pinia';
import { defaultTheme } from '@/utils/config';
import { writeText, readText } from 'clipboard-polyfill';
import { ref, nextTick } from 'vue';

import { Terminal } from '@xterm/xterm';
import { formatMessage } from '@/utils';
import { FitAddon } from '@xterm/addon-fit';
import { FORMATTER_MESSAGE_TYPE } from '@/types/modules/message.type'
import { SearchAddon } from '@xterm/addon-search';
import { useConnectionStore,  } from '@/store/modules/useConnection';
import { useTerminalSettingsStore } from '@/store/modules/terminalSettings';


/**
 * @description 终端控制器
 * @param config
 */
export const useTerminalInstance = (socket?: WebSocket | '') => {
  const fitAddon = new FitAddon();
  const searchAddon = new SearchAddon();

  const terminalSelectionText = ref<string>('');
  const terminalInstance = ref<Terminal>();


  const connectionStore = useConnectionStore();
  const terminalSettingsStore = useTerminalSettingsStore();


  /**
   * @description 加载终端适配器以及初始化载终端事件
   * @param terminal
   */
  const initializeTerminalEvent = (terminal: Terminal) => {
    terminal.loadAddon(fitAddon);
    terminal.loadAddon(searchAddon);

    terminal.onSelectionChange(async () => {
      terminalSelectionText.value = terminal.getSelection().trim();

      if (!terminalSelectionText.value) {
        return;
      }

      await writeText(terminalSelectionText.value);
    });
  };

  /**
   * @description 初始化元素事件
   * @param el
   */
  const initializeElementEvent = (el: HTMLElement) => {
    el.addEventListener('mouseenter', () => {
      fitAddon.fit();
      terminalInstance.value?.focus();
    });

    el.addEventListener(
      'contextmenu',
      async (e: MouseEvent) => {
        if (e.ctrlKey || terminalSettingsStore.quickPaste !== '1') return;

        e.preventDefault();

        let text: string = '';

        try {
          text = await readText();
        } catch (e) {
          terminalSelectionText.value ? (text = terminalSelectionText.value) : '';
        }

        const conn = Array.from(connectionStore.connectionStateMap.values())[0] || {};

        if (!conn.socket) {
          return;
        }

        conn.socket.send(formatMessage(conn.terminalId || '', FORMATTER_MESSAGE_TYPE.TERMINAL_DATA, text));
      },
      false
    );
  };

  /**
   * @description  创建终端实例
   */
  const createTerminalInstance = (el: HTMLElement): Terminal => {
    // terminal 设置
    const { fontSize, lineHeight, fontFamily, theme } = storeToRefs(terminalSettingsStore);

    // 创建终端实例
    terminalInstance.value = new Terminal({
      allowProposedApi: true,
      rightClickSelectsWord: true,
      scrollback: 5000,
      theme: defaultTheme,
      fontSize: fontSize?.value,
      lineHeight: lineHeight?.value,
      fontFamily: fontFamily?.value
    });

    // 添加适配器以及初始化终端事件
    initializeElementEvent(el);
    initializeTerminalEvent(terminalInstance.value);

    // 终端的实际 open 交由组件控制
    return terminalInstance.value;
  };
  /**
   * @description 设置终端主题
   */
  const setTerminalTheme = (themeName: string) => {
    const theme = xtermTheme[themeName] || defaultTheme;

    nextTick(() => {
      terminalInstance.value!.options.theme = theme;
    });
  };

  return {
    fitAddon,
    setTerminalTheme,
    createTerminalInstance
  };
};
