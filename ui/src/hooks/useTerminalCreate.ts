import { nextTick, ref } from 'vue';
import xtermTheme from 'xterm-theme';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { useDebounceFn } from '@vueuse/core';
import { writeText } from 'clipboard-polyfill';
import { SearchAddon } from '@xterm/addon-search';

import { formatMessage } from '@/utils';
import { defaultTheme } from '@/utils/config';
import { lunaCommunicator } from '@/utils/lunaBus';
import { getDefaultTerminalConfig } from '@/utils/guard';
import { FORMATTER_MESSAGE_TYPE, LUNA_MESSAGE_TYPE } from '@/types/modules/message.type';

export const useTerminalCreate = () => {
  // 会话状态和连接信息
  const sessionId = ref<string>('');
  const terminalId = ref<string>('');

  // 终端实例
  const terminalInstance = ref<Terminal>();

  // 终端交互状态
  const terminalSelectionText = ref<string>('');

  // 插件
  const fitAddon = new FitAddon();
  const searchAddon = new SearchAddon();

  const defaultTerminalCfg = getDefaultTerminalConfig();

  const debouncedResize = useDebounceFn(({ cols, rows, socket }) => {
    if (!fitAddon) return;

    nextTick(() => {
      fitAddon.fit();

      const resizeData = JSON.stringify({ cols, rows });

      socket.send(formatMessage(terminalId.value, FORMATTER_MESSAGE_TYPE.TERMINAL_RESIZE, resizeData));
    });
  }, 200);
  const debouncedSendLunaKey = useDebounceFn((key: string) => {
    switch (key) {
      case 'ArrowRight':
        lunaCommunicator.sendLuna(LUNA_MESSAGE_TYPE.KEYEVENT, 'alt+shift+right');
        break;
      case 'ArrowLeft':
        lunaCommunicator.sendLuna(LUNA_MESSAGE_TYPE.KEYEVENT, 'alt+shift+left');
        break;
    }
  }, 500);

  const terminalTheme = (themeName: string) => {
    if (!xtermTheme[themeName]) {
      return defaultTheme;
    }
    return xtermTheme[themeName];
  };

  const terminalEvent = (socket: WebSocket) => {
    terminalInstance.value?.attachCustomKeyEventHandler((e: KeyboardEvent) => {
      if (e.altKey && e.shiftKey && (e.key === 'ArrowRight' || e.key === 'ArrowLeft')) {
        debouncedSendLunaKey(e.key);
        return false;
      }

      if (e.ctrlKey && e.key === 'c' && terminalInstance.value?.hasSelection()) {
        return false;
      }

      return !(e.ctrlKey && e.key === 'v');
    });
    terminalInstance.value?.onResize(({ cols, rows }) => debouncedResize({ cols, rows, socket }));
    terminalInstance.value?.onSelectionChange(async () => {
      terminalSelectionText.value = terminalInstance.value?.getSelection() || '';

      if (!terminalSelectionText.value) {
        return;
      }

      await writeText(terminalSelectionText.value);
    });
  };

  const createTerminal = () => {
    const terminal = new Terminal({
      allowProposedApi: true,
      rightClickSelectsWord: true,
      scrollback: 5000,
      theme: terminalTheme(defaultTerminalCfg.themeName),
      fontSize: defaultTerminalCfg.fontSize,
      lineHeight: defaultTerminalCfg.lineHeight,
      fontFamily: defaultTerminalCfg.fontFamily,
    });

    terminal.loadAddon(fitAddon);
    terminal.loadAddon(searchAddon);

    terminalInstance.value = terminal;

    return terminal;
  };

  return {
    sessionId,
    terminalId,
    terminalSelectionText,

    fitAddon,

    terminalEvent,
    terminalTheme,
    createTerminal,
  };
};
