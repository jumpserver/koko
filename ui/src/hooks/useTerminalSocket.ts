import type { ConfigProviderProps } from 'naive-ui';

import { useI18n } from 'vue-i18n';
import xtermTheme from 'xterm-theme';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { writeText } from 'clipboard-polyfill';
import { createDiscreteApi, darkTheme } from 'naive-ui';
import { useDebounceFn, useWebSocket, useWindowSize } from '@vueuse/core';
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue';

import { lunaCommunicator } from '@/utils/lunaBus';
import { formatMessage, preprocessInput } from '@/utils';
import { getDefaultTerminalConfig } from '@/utils/guard';
import { defaultTheme, MaxTimeout } from '@/utils/config';
import { useTerminalSettingsStore } from '@/store/modules/terminalSettings';
import { FORMATTER_MESSAGE_TYPE, LUNA_MESSAGE_TYPE } from '@/types/modules/message.type';

import { generateWsURL } from './helper';

const isSocketClosing = (socket: WebSocket) => {
  return socket.readyState === WebSocket.CLOSING || socket.readyState === WebSocket.CLOSED;
};

const useTerminalSocket = (el: HTMLElement) => {
  const { t } = useI18n();
  const { width, height } = useWindowSize();

  const defaultTerminalCfg = getDefaultTerminalConfig();
  const terminalSettingsStore = useTerminalSettingsStore();

  const fitAddon = new FitAddon();

  const terminalId = ref('');
  const selectionText = ref('');

  const lastSendTime = ref(new Date());
  const lastReceiveTime = ref(new Date());

  const terminalRef = ref<Terminal | null>(null);
  const socketRef = ref<WebSocket | null>(null);
  const pingInterval = ref<ReturnType<typeof setInterval> | null>(null);

  const configProviderPropsRef = computed<ConfigProviderProps>(() => ({
    theme: darkTheme,
  }));

  const autoTerminalFit = watch([width.value, height.value], ([_newWidth, _newHeight]: [number, number]) => {
    if (!terminalRef.value || !fitAddon) return;

    nextTick(() => {
      fitAddon.fit();
    });
  });

  const { message } = createDiscreteApi(['message'], {
    configProviderProps: configProviderPropsRef,
  });

  const debouncedResize = useDebounceFn(({ cols, rows }) => {
    if (!fitAddon || !socketRef.value) return;

    fitAddon.fit();

    const resizeData = JSON.stringify({ cols, rows });
    socketRef.value.send(formatMessage(terminalId.value, FORMATTER_MESSAGE_TYPE.TERMINAL_RESIZE, resizeData));
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

  /**
   * @description 获取终端主题
   * @param {string} themeName
   */
  const terminalTheme = (themeName: string) => {
    if (!xtermTheme[themeName]) {
      return defaultTheme;
    }
    return xtermTheme[themeName];
  };

  /**
   * @description 监听 socket 事件
   */
  const listenSocketEvent = () => {
    if (!socketRef.value) {
      return;
    }

    socketRef.value.onopen = () => {
      if (pingInterval.value) clearInterval(pingInterval.value);

      pingInterval.value = setInterval(() => {
        if (isSocketClosing(socketRef.value!)) {
          return clearInterval(pingInterval.value!);
        }

        const currentDate = new Date();
        const pongTimeout: number = currentDate.getTime() - lastReceiveTime.value.getTime() - MaxTimeout;
        const pingTimeout: number = currentDate.getTime() - lastSendTime.value.getTime() - MaxTimeout;

        // 已经超时
        if (pingTimeout < 0 && pongTimeout < 0) {
          return clearInterval(pingInterval.value!);
        }

        socketRef.value!.send(formatMessage('', FORMATTER_MESSAGE_TYPE.PING, ''));
      });
    };
    socketRef.value.onclose = () => {
      if (!terminalRef.value) return;

      terminalRef.value.write(`\x1B[31m${t('terminal.websocket.closed')}\x1B[0m`);
    };
    socketRef.value.onmessage = (message: MessageEvent) => {
      lastReceiveTime.value = new Date();
    };
  };

  /**
   * @description 监听 terminalRef 事件
   */
  const listenTerminalRefEvent = () => {
    if (!terminalRef.value || !socketRef.value) {
      return;
    }

    terminalRef.value.onData((data: string) => {
      lastSendTime.value = new Date();

      if (isSocketClosing(socketRef.value!)) {
        return;
      }

      const processedData = preprocessInput(data, terminalSettingsStore.getConfig);
      socketRef.value!.send(formatMessage('', FORMATTER_MESSAGE_TYPE.TERMINAL_DATA, processedData));
    });
    terminalRef.value.onResize(({ cols, rows }) => debouncedResize({ cols, rows }));
    terminalRef.value.onSelectionChange(async () => {
      selectionText.value = terminalRef.value!.getSelection() || '';

      if (!selectionText.value) {
        return;
      }

      await writeText(selectionText.value);
    });
    terminalRef.value.attachCustomKeyEventHandler((e: KeyboardEvent) => {
      if (e.altKey && e.shiftKey && (e.key === 'ArrowRight' || e.key === 'ArrowLeft')) {
        debouncedSendLunaKey(e.key);
        return false;
      }

      // 允许复制操作而不是发送中断信号
      if (e.ctrlKey && e.key === 'c' && terminalRef.value?.hasSelection()) {
        return false;
      }

      // 阻止默认的粘贴行为，粘贴数据通过 socket 写入
      return !(e.ctrlKey && e.key === 'v');
    });
  };

  /**
   * @description 创建终端
   */
  const createTerminal = () => {
    const terminal: Terminal = new Terminal({
      // 基础配置
      fontSize: defaultTerminalCfg.fontSize,
      fontFamily: defaultTerminalCfg.fontFamily,
      lineHeight: defaultTerminalCfg.lineHeight,

      // 光标配置
      cursorBlink: true,
      cursorStyle: 'block',
      rightClickSelectsWord: true,

      // 滚动配置
      scrollback: 5000,
      scrollOnUserInput: true,

      // 主题配置
      theme: terminalTheme(defaultTerminalCfg.themeName),

      // 性能优化
      allowProposedApi: true,
      customGlyphs: true,
    });

    terminal.loadAddon(fitAddon);
    terminal.open(el);

    fitAddon.fit();

    terminalRef.value = terminal;
  };

  /**
   * @description 创建 WebSocket 连接
   */
  const createWebSocket = () => {
    const url = generateWsURL();

    const { ws } = useWebSocket(url, {
      protocols: ['JMS-KOKO'],
      autoReconnect: {
        retries: 5,
        delay: 3000,
      },
    });

    if (!ws.value) {
      return message.error('Failed to create WebSocket connection');
    }

    ws.value.binaryType = 'arraybuffer';

    socketRef.value = ws.value;
  };

  onMounted(() => {
    if (!el) return;

    createTerminal();
    createWebSocket();

    nextTick(() => {
      listenSocketEvent();
      listenTerminalRefEvent();
    });
  });

  onUnmounted(() => {
    autoTerminalFit();
  });
};
