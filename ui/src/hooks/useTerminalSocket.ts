import type { ConfigProviderProps } from 'naive-ui';
import type { Sentry } from 'nora-zmodemjs/src/zmodem_browser';

import { useI18n } from 'vue-i18n';
import xtermTheme from 'xterm-theme';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { SearchAddon } from '@xterm/addon-search';
import { createDiscreteApi, darkTheme } from 'naive-ui';
import { readText, writeText } from 'clipboard-polyfill';
import { useDebounceFn, useWebSocket, useWindowSize } from '@vueuse/core';
import { computed, nextTick, onMounted, onUnmounted, ref, shallowRef, watch } from 'vue';

import type { SettingConfig } from '@/types/modules/config.type';
import type { OnlineUser, ShareUserOptions } from '@/types/modules/user.type';

import { lunaCommunicator } from '@/utils/lunaBus';
import { getDefaultTerminalConfig } from '@/utils/guard';
import { defaultTheme, MaxTimeout } from '@/utils/config';
import { useConnectionStore } from '@/store/modules/useConnection';
import { useTerminalSettingsStore } from '@/store/modules/terminalSettings';
import { formatMessage, preprocessInput, writeBufferToTerminal } from '@/utils';
import {
  FORMATTER_MESSAGE_TYPE,
  LUNA_MESSAGE_TYPE,
  MESSAGE_TYPE,
  ZMODEM_ACTION_TYPE,
} from '@/types/modules/message.type';

import { useZmodem } from './useZmodem';
import { generateWsURL, updateIcon } from './helper';
import { useTerminalEvents } from './useTerminalEvents';
import { getXTerminalLineContent } from './helper/index';

/**
 * @description 判断 WebSocket 是否关闭
 * @param {WebSocket} socket
 * @returns {boolean}
 */
const isSocketClosing = (socket: WebSocket) => {
  return socket.readyState === WebSocket.CLOSING || socket.readyState === WebSocket.CLOSED;
};

/**
 * @description 获取终端主题
 * @param {string} themeName
 */
export const terminalTheme = (themeName: string) => {
  if (!xtermTheme[themeName]) {
    return defaultTheme;
  }
  return xtermTheme[themeName];
};

export const useTerminalSocket = () => {
  let sentry: Sentry | null = null;

  const { t } = useI18n();
  const { createSentry } = useZmodem();
  const { width, height } = useWindowSize();

  const { sendLunaEvent, emitTerminalConnect, emitTerminalSession } = useTerminalEvents();

  const containerRef = shallowRef<HTMLElement>();

  const shareId = ref('');
  const shareCode = ref('');
  const sessionId = ref('');
  const terminalId = ref('');
  const selectionText = ref('');
  const zmodemTransferStatus = ref(true);

  const lastSendTime = ref(new Date());
  const lastReceiveTime = ref(new Date());

  const onlineUsers = ref<OnlineUser[]>([]);
  const userOptions = ref<ShareUserOptions[]>([]);

  const terminalRef = ref<Terminal | null>(null);
  const socketRef = ref<WebSocket | null>(null);
  const featureSetting = ref<Partial<SettingConfig>>({});
  const pingInterval = ref<ReturnType<typeof setInterval> | null>(null);
  const warningInterval = ref<ReturnType<typeof setInterval> | null>(null);

  const connectionStore = useConnectionStore();
  const defaultTerminalCfg = getDefaultTerminalConfig();
  const terminalSettingsStore = useTerminalSettingsStore();

  const fitAddon = new FitAddon();
  const searchAddon = new SearchAddon();

  const configProviderPropsRef = computed<ConfigProviderProps>(() => ({
    theme: darkTheme,
  }));

  const autoTerminalFit = watch([width, height], ([_newWidth, _newHeight]: [number, number]) => {
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
   * @description 分发 Socket 消息
   */
  const dispatch = (socketData: string) => {
    if (!socketData || !socketRef.value || !terminalRef.value) return;

    const parsedMessageData = JSON.parse(socketData);

    switch (parsedMessageData.type) {
      case MESSAGE_TYPE.CLOSE: {
        connectionStore.updateConnectionState({
          enableShare: false,
          onlineUsers: [],
        });

        socketRef.value.close();
        sendLunaEvent(LUNA_MESSAGE_TYPE.CLOSE, '');
        break;
      }
      case MESSAGE_TYPE.ERROR: {
        terminalRef.value!.write(parsedMessageData.err);
        sendLunaEvent(LUNA_MESSAGE_TYPE.TERMINAL_ERROR, '');
        break;
      }
      case MESSAGE_TYPE.PING: {
        break;
      }
      case MESSAGE_TYPE.CONNECT: {
        terminalId.value = parsedMessageData.id;
        emitTerminalConnect(terminalId.value);

        connectionStore.setConnectionState({
          socket: socketRef.value!,
          terminal: terminalRef.value!,
          terminalId: parsedMessageData.id,
        });

        const terminalData = {
          cols: terminalRef.value!.cols,
          rows: terminalRef.value!.rows,
          code: connectionStore.shareCode,
        };

        const info = JSON.parse(parsedMessageData.data);

        featureSetting.value = info.setting;

        updateIcon(info.setting);

        socketRef.value!.send(
          formatMessage(terminalId.value, FORMATTER_MESSAGE_TYPE.TERMINAL_INIT, JSON.stringify(terminalData))
        );

        break;
      }
      case MESSAGE_TYPE.TERMINAL_ERROR: {
        terminalRef.value!.write(parsedMessageData.err);
        break;
      }
      case MESSAGE_TYPE.MESSAGE_NOTIFY: {
        const eventName = JSON.parse(parsedMessageData.data).event_name;

        if (eventName === 'sync_user_preference') {
          message.success(t('ThemeSyncSuccessful'));
        }

        break;
      }
      case MESSAGE_TYPE.TERMINAL_SHARE: {
        const data = JSON.parse(parsedMessageData.data);

        shareId.value = data.share_id;
        shareCode.value = data.code;

        connectionStore.updateConnectionState({
          shareId: data.share_id,
          shareCode: data.code,
        });

        break;
      }
      case MESSAGE_TYPE.TERMINAL_ACTION: {
        const actionType = parsedMessageData.data;

        switch (actionType) {
          case ZMODEM_ACTION_TYPE.ZMODEM_START: {
            zmodemTransferStatus.value = true;
            break;
          }
          case ZMODEM_ACTION_TYPE.ZMODEM_END: {
            terminalRef.value!.write('\r\n');
            break;
          }
          default: {
            zmodemTransferStatus.value = false;
          }
        }

        break;
      }
      case MESSAGE_TYPE.TERMINAL_SESSION: {
        const sessionInfo = JSON.parse(parsedMessageData.data);
        const sessionDetail = sessionInfo.session;

        emitTerminalSession(sessionInfo);

        const share = sessionInfo?.permission?.actions?.includes('share');

        if (sessionInfo.backspaceAsCtrlH) {
          const value = sessionInfo.backspaceAsCtrlH ? '1' : '0';

          terminalSettingsStore.setDefaultTerminalConfig('backspaceAsCtrlH', value);
        }

        if (sessionInfo.ctrlCAsCtrlZ) {
          const value = sessionInfo.ctrlCAsCtrlZ ? '1' : '0';

          terminalSettingsStore.setDefaultTerminalConfig('ctrlCAsCtrlZ', value);
        }

        if (sessionInfo.themeName) {
          const theme = terminalTheme(sessionInfo.themeName);

          nextTick(() => {
            terminalRef.value!.options.theme = theme;
          });
        }

        if (featureSetting.value.SECURITY_SESSION_SHARE && share) {
          connectionStore.updateConnectionState({
            enableShare: true,
          });
        }

        sessionId.value = sessionDetail.id;
        connectionStore.updateConnectionState({
          sessionId: sessionDetail.id,
        });
        terminalSettingsStore.setDefaultTerminalConfig('theme', sessionInfo.themeName);

        break;
      }
      case MESSAGE_TYPE.TERMINAL_SHARE_JOIN: {
        const data = JSON.parse(parsedMessageData.data);

        // data 中如果 primary 为 true 则表示是当前用户
        onlineUsers.value.push(data);

        connectionStore.updateConnectionState({
          onlineUsers: onlineUsers.value,
        });
        sendLunaEvent(LUNA_MESSAGE_TYPE.SHARE_USER_ADD, JSON.stringify({ ...data, sessionId: sessionId.value }));

        if (!data.primary) {
          message.info(`${data.user} ${t('JoinShare')}`);
        }

        break;
      }
      case MESSAGE_TYPE.TERMINAL_PERM_VALID: {
        clearInterval(warningInterval.value!);
        message.info(`${t('PermissionValid')}`);
        break;
      }
      case MESSAGE_TYPE.TERMINAL_SHARE_LEAVE: {
        const data: OnlineUser = JSON.parse(parsedMessageData.data);

        sendLunaEvent(LUNA_MESSAGE_TYPE.SHARE_USER_LEAVE, parsedMessageData.data);

        const index = onlineUsers.value.findIndex(item => item.user_id === data.user_id && !item.primary);

        if (index !== -1) {
          onlineUsers.value.splice(index, 1);

          connectionStore.updateConnectionState({
            onlineUsers: onlineUsers.value,
          });

          message.info(`${data.user} ${t('LeaveShare')}`);
        }
        break;
      }
      case MESSAGE_TYPE.TERMINAL_PERM_EXPIRED: {
        const data = JSON.parse(parsedMessageData.data);
        const warningMsg = `${t('PermissionExpired')}: ${data.detail}`;

        message.warning(warningMsg);

        if (warningInterval.value) {
          clearInterval(warningInterval.value);
        }
        warningInterval.value = setInterval(() => {
          message.warning(warningMsg);
        }, 1000 * 60);
        break;
      }
      case MESSAGE_TYPE.TERMINAL_SESSION_PAUSE: {
        const data = JSON.parse(parsedMessageData.data);

        message.info(`${data.user} ${t('PauseSession')}`);
        break;
      }
      case MESSAGE_TYPE.TERMINAL_GET_SHARE_USER: {
        userOptions.value = JSON.parse(parsedMessageData.data);

        connectionStore.updateConnectionState({
          userOptions: userOptions.value,
        });

        break;
      }
      case MESSAGE_TYPE.TERMINAL_SESSION_RESUME: {
        const data = JSON.parse(parsedMessageData.data);

        message.info(`${data.user} ${t('ResumeSession')}`);
        break;
      }
      case MESSAGE_TYPE.TERMINAL_SHARE_USER_REMOVE: {
        message.info(t('RemoveShareUser'));
        socketRef.value!.close();
        break;
      }
    }
  };

  /**
   * @description 终端 zmodem 处理二进制消息
   * @param {MessageEvent} socketMessage
   */
  const handleBinaryMessage = (socketMessage: MessageEvent) => {
    if (!terminalRef.value || !sentry) {
      return;
    }

    if (zmodemTransferStatus.value) {
      try {
        sentry.consume(socketMessage.data);
      } catch (_e) {
        if (sentry.get_confirmed_session()) {
          sentry.get_confirmed_session()?.abort();
          message.error('File transfer error, file transfer interrupted');
        }
      }
    } else {
      writeBufferToTerminal(true, false, terminalRef.value, socketMessage.data);
    }
  };

  /**
   * @description 监听 socket 事件
   */
  const listenSocketEvent = () => {
    if (!socketRef.value) {
      return;
    }

    sentry = createSentry(terminalRef.value!, socketRef.value!, lastSendTime);

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

      terminalRef.value.write(`\r\n`);
      terminalRef.value.write(`\x1B[31m${t('Terminal websocket closed')}\x1B[0m`);
    };
    socketRef.value.onmessage = (message: MessageEvent) => {
      lastReceiveTime.value = new Date();

      if (typeof message.data === 'object') {
        handleBinaryMessage(message);
      } else {
        dispatch(message.data);
      }
    };
  };

  /**
   * @description 监听挂载节点事件
   */
  const listenElEvent = () => {
    if (!terminalRef.value) {
      return;
    }

    containerRef.value!.addEventListener('click', () => {
      sendLunaEvent(LUNA_MESSAGE_TYPE.CLICK, '');
    });
    containerRef.value!.addEventListener('mouseenter', () => {
      fitAddon.fit();
      terminalRef.value!.focus();
    });
    containerRef.value!.addEventListener('contextmenu', async (e: MouseEvent) => {
      // 只有在开启右键快速复制时才允许粘贴
      // TODO 对于 terminal 的 contextmenu 后续需要进行封装
      if (e.ctrlKey || terminalSettingsStore.quickPaste !== '1') return;

      e.preventDefault();

      let text: string = '';

      try {
        text = await readText();
      } catch (_e) {
        if (selectionText.value) {
          text = selectionText.value;
        }
      }

      if (!text) {
        return;
      }

      if (isSocketClosing(socketRef.value!)) {
        return message.error(t('WebSocket connection is closed, please refresh the page'));
      }

      socketRef.value!.send(formatMessage(terminalId.value, FORMATTER_MESSAGE_TYPE.TERMINAL_DATA, text));
    });
    containerRef.value!.addEventListener('mouseleave', () => {
      terminalRef.value?.blur();

      sendLunaEvent(LUNA_MESSAGE_TYPE.TERMINAL_CONTENT_RESPONSE, {
        content: getXTerminalLineContent(10, terminalRef.value!),
        sessionId: sessionId.value,
        terminalId: terminalId.value,
      });
    });
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
    terminal.loadAddon(searchAddon);

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
    if (!containerRef.value) return;

    createTerminal();
    createWebSocket();

    nextTick(() => {
      listenSocketEvent();
      listenTerminalRefEvent();
      listenElEvent();

      terminalRef.value?.open(containerRef.value!);

      fitAddon.fit();
    });
  });

  onUnmounted(() => {
    autoTerminalFit();
  });

  return {
    containerRef,
  };
};
