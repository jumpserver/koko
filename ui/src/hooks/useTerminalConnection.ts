import { useI18n } from 'vue-i18n';
import { Terminal } from '@xterm/xterm';
import { ref, computed, watch } from 'vue';
import { darkTheme, createDiscreteApi } from 'naive-ui';
import mitt from 'mitt'
import { MaxTimeout } from '@/utils/config';
import { preprocessInput } from '@/utils';
import { useSentry } from '@/hooks/useZsentry';
import { Sentry } from 'nora-zmodemjs/src/zmodem_browser';
import { updateIcon } from '@/hooks/helper';
import { useConnectionStore } from '@/store/modules/useConnection';
import { useTerminalSettingsStore } from '@/store/modules/terminalSettings';
import {  formatMessage, writeBufferToTerminal } from '@/utils';
import { FORMATTER_MESSAGE_TYPE, LUNA_MESSAGE_TYPE, MESSAGE_TYPE, SEND_LUNA_MESSAGE_TYPE, ZMODEM_ACTION_TYPE } from '@/types/modules/message.type';

import type { FitAddon } from '@xterm/addon-fit';
import type { ConfigProviderProps } from 'naive-ui';
import type { SettingConfig } from '@/types/modules/config.type';
import type { OnlineUser } from '@/types/modules/user.type';
import type { ShareUserOptions } from '@/types/modules/user.type';
import type { TerminalSessionInfo } from '@/types/modules/postmessage.type';

export const eventBus = mitt<{
  'luna-event': { event: string; data: string };
  'terminal-session': TerminalSessionInfo,
}>();
// 修改 sendEventToLuna 函数
const sendLunaEvent = (event: string, data: string) => {
  eventBus.emit('luna-event', { event, data })
}

export const useTerminalConnection = () => {
  let sentry: Sentry;

  const onlineUsers = ref<OnlineUser[]>([]);

  const shareId = ref<string>('');
  const shareCode = ref<string>('');
  const sessionId = ref<string>('');
  const terminalId = ref<string>('');
  const pingInterval = ref<number | null>(null);
  const warningInterval = ref<number | null>(null);
  const enableShare = ref<boolean>(false);

  const zmodemTransferStatus = ref<boolean>(true);

  const lastSendTime = ref(new Date());
  const lastReceiveTime = ref(new Date());
  const userOptions = ref<ShareUserOptions[]>([]);
  const featureSetting = ref<Partial<SettingConfig>>({});

  const connectionStore = useConnectionStore();
  const terminalSettingsStore = useTerminalSettingsStore();

  const configProviderPropsRef = computed<ConfigProviderProps>(() => ({
    theme: darkTheme
  }));

  const { message } = createDiscreteApi(['message'], {
    configProviderProps: configProviderPropsRef
  });
  /**
   * @description 创建 ZMODEM 实例
   * @param terminal
   * @param socket
   */
  const createZmodemInstance = (terminal: Terminal, socket: WebSocket) => {
    const { t } = useI18n();
    const { createSentry } = useSentry(lastSendTime, t);

    sentry = createSentry(socket, terminal);
  };
  /**
   * @description 心跳
   * @param socket
   */
  const heartBeat = (socket: WebSocket) => {
    if (pingInterval.value) clearInterval(pingInterval.value);

    pingInterval.value = setInterval(() => {
      // 如果 socket 已经关闭，则停止心跳
      if (socket.CLOSED === socket.readyState || socket.CLOSING === socket.readyState) {
        return clearInterval(pingInterval.value!);
      }

      const currentDate = new Date();

      if (lastReceiveTime.value.getTime() - currentDate.getTime() > MaxTimeout) {
        socket.close();
      }

      const pingTimeout: number = currentDate.getTime() - lastSendTime.value.getTime();

      if (pingTimeout < 0) return;

      socket.send(formatMessage('', FORMATTER_MESSAGE_TYPE.PING, ''));
    }, 25 * 1000);
  };
  /**
   * @description 分发消息 在 dispatch 中处理所有的消息类型，然后需要什么就通过 hook 返回到组件中
   * @param data
   * @param terminal
   * @param socket
   */
  const dispatch = (data: string, terminal: Terminal, socket: WebSocket, t: any) => {
    if (!data) return;

    const parsedMessageData = JSON.parse(data);

    switch (parsedMessageData.type) {
      case MESSAGE_TYPE.CLOSE: {
        enableShare.value = false;
        onlineUsers.value = [];

        connectionStore.updateConnectionState(terminalId.value, {
          enableShare: false,
          onlineUsers: []
        });

        socket.close();

        // sendEventToLuna(SEND_LUNA_MESSAGE_TYPE.CLOSE, '', lunaId.value, origin.value);
        sendLunaEvent(SEND_LUNA_MESSAGE_TYPE.CLOSE, '');
        break;
      }
      case MESSAGE_TYPE.ERROR: {
        terminal.write(parsedMessageData.err);

        // sendEventToLuna(SEND_LUNA_MESSAGE_TYPE.TERMINAL_ERROR, '', lunaId.value, origin.value)
        sendLunaEvent(SEND_LUNA_MESSAGE_TYPE.TERMINAL_ERROR, '');
        break;
      }
      case MESSAGE_TYPE.PING: {
        break;
      }
      case MESSAGE_TYPE.CONNECT: {
        terminalId.value = parsedMessageData.id;

        connectionStore.setConnectionState(terminalId.value, {
          socket: socket,
          terminal: terminal,
          terminalId: parsedMessageData.id
        });

        // watchEffect(() => {
        //   connectionStore.updateConnectionState(terminalId.value, {
        //     lunaId: lunaId.value,
        //     origin: origin.value
        //   });
        // });

        const terminalData = {
          cols: terminal.cols,
          rows: terminal.rows,
          code: shareCode.value
        };

        const info = JSON.parse(parsedMessageData.data);

        featureSetting.value = info.setting;

        updateIcon(info.setting);

        socket.send(
          formatMessage(terminalId.value, FORMATTER_MESSAGE_TYPE.TERMINAL_INIT, JSON.stringify(terminalData))
        );
        break;
      }
      case MESSAGE_TYPE.TERMINAL_ERROR: {
        terminal.write(parsedMessageData.err);
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

        console.log('TERMINAL_SHARE', data);

        shareId.value = data.share_id;
        shareCode.value = data.code;
        // todo 这里需要处理一下 shareId 和 shareCode 的 更新到 luna event
        connectionStore.updateConnectionState(terminalId.value, {
          shareId: data.share_id,
          shareCode: data.code
        });
        sendLunaEvent(LUNA_MESSAGE_TYPE.SHARE_CODE_RESPONSE, JSON.stringify(data));
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
            terminal.write('\r\n');
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
        eventBus.emit('terminal-session', sessionInfo);

        const share = sessionInfo?.permission?.actions?.includes('share');

        if (sessionInfo.backspaceAsCtrlH) {
          const value = sessionInfo.backspaceAsCtrlH ? '1' : '0';

          terminalSettingsStore.setDefaultTerminalConfig('backspaceAsCtrlH', value);
        }

        if (sessionInfo.ctrlCAsCtrlZ) {
          const value = sessionInfo.ctrlCAsCtrlZ ? '1' : '0';

          terminalSettingsStore.setDefaultTerminalConfig('ctrlCAsCtrlZ', value);
        }

        if (featureSetting.value.SECURITY_SESSION_SHARE && share) {
          enableShare.value = true;

          connectionStore.updateConnectionState(terminalId.value, {
            enableShare: true
          });
        }

        sessionId.value = sessionDetail.id;
        connectionStore.updateConnectionState(terminalId.value, {
          sessionId: sessionDetail.id
        });
        terminalSettingsStore.setDefaultTerminalConfig('theme', sessionInfo.themeName);

        break;
      }
      case MESSAGE_TYPE.TERMINAL_SHARE_JOIN: {
        const data = JSON.parse(parsedMessageData.data);

        // data 中如果 primary 为 true 则表示是当前用户
        onlineUsers.value.push(data);

        connectionStore.updateConnectionState(terminalId.value, {
          onlineUsers: onlineUsers.value
        });

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

        const index = onlineUsers.value.findIndex(item => item.user_id === data.user_id && !item.primary);

        if (index !== -1) {
          onlineUsers.value.splice(index, 1);

          connectionStore.updateConnectionState(terminalId.value, {
            onlineUsers: onlineUsers.value
          });

          message.info(`${data.user} ${t('LeaveShare')}`);
        }
        break;
      }
      case MESSAGE_TYPE.TERMINAL_PERM_EXPIRED: {
        const data = JSON.parse(parsedMessageData.data);
        const warningMsg = `${t('PermissionExpired')}: ${data.detail}`;

        message.warning(warningMsg);

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

        connectionStore.updateConnectionState(terminalId.value, {
          userOptions: userOptions.value
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
        socket.close();
        break;
      }
    }
  };
  /**
   * @description 处理二进制消息
   * @param event
   * @param terminal
   */
  const handleBinaryMessage = (event: MessageEvent, terminal: Terminal) => {
    if (zmodemTransferStatus.value) {
      try {
        sentry.consume(event.data);
      } catch (e) {
        if (sentry.get_confirmed_session()) {
          sentry.get_confirmed_session()?.abort();
          message.error('File transfer error, file transfer interrupted');
        }
      }
    } else {
      writeBufferToTerminal(true, false, terminal, event.data);
    }
  };
  /**
   * @description 设置分享代码
   * @param code
   */
  const setShareCode = (code: string) => {
    shareCode.value = code;

    connectionStore.updateConnectionState(terminalId.value, {
      shareCode: code
    });
  };
  /**
   * @description 获取指定分享用户
   * @param socket
   * @param query
   * @returns
   */
  const getShareUser = (socket: WebSocket, query: any): Promise<ShareUserOptions[]> => {
    return new Promise(resolve => {
      socket.send(
        formatMessage(terminalId.value, FORMATTER_MESSAGE_TYPE.TERMINAL_GET_SHARE_USER, JSON.stringify({ query }))
      );

      watch(
        () => userOptions.value,
        newUserOptions => {
          resolve(newUserOptions);
        },
        { immediate: true }
      );
    });
  };

  /**
   * @description 终端 resize 事件
   * @param terminalInstance
   * @param socket
   */
  const terminalResizeEvent = (terminalInstance: Terminal, socket: WebSocket, fitAddon: FitAddon) => {
    if (!socket) {
      return;
    }

    terminalInstance.onResize(({ cols, rows }) => {
      fitAddon.fit();

      const resizeData = JSON.stringify({ cols, rows });
      socket.send(formatMessage(terminalId.value, FORMATTER_MESSAGE_TYPE.TERMINAL_RESIZE, resizeData));
    });
  };

  /**
   * @description 初始化 socket 事件
   * @param terminal
   * @param socket
   */
  const initializeSocketEvent = (terminal: Terminal, socket: WebSocket, t: any) => {
    // 创建 ZMODEM 实例
    createZmodemInstance(terminal, socket);

    socket.onopen = () => {
      socket.binaryType = 'arraybuffer';
      heartBeat(socket);
    };
    socket.onclose = () => {
      terminal.write('\r\n');
      terminal.write('\r\n');
      terminal.write('\x1b[31mConnection websocket has been closed\x1b[0m');
    };
    socket.onerror = () => {
      // terminal.write('\x1b[31mConnection Websocket Error Occurred\x1b[0m');
      // 换行
      // terminal.write('\r\n');
      // terminal.write('\r\n');
    };
    socket.onmessage = (event: MessageEvent) => {
      lastReceiveTime.value = new Date();

      if (typeof event.data === 'object') {
        handleBinaryMessage(event, terminal);
      } else {
        dispatch(event.data, terminal, socket, t);
      }
    };

    terminal.onData((data: string) => {
      const processedData = preprocessInput(data, terminalSettingsStore.getConfig);

      lastSendTime.value = new Date();

      sendLunaEvent('KEYBOARDEVENT', '');

      socket.send(formatMessage(terminalId.value, FORMATTER_MESSAGE_TYPE.TERMINAL_DATA, processedData));
    });

    // terminal.attachCustomKeyEventHandler((e: KeyboardEvent) => {
    //   return handleCustomKey(e, terminal, lunaId.value, origin.value);
    // });
  };

  return {
    getShareUser,
    setShareCode,
    terminalResizeEvent,
    initializeSocketEvent,
    eventBus,
  };
};
