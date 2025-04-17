import { useI18n } from 'vue-i18n';
import { Terminal } from '@xterm/xterm';
import { ref, computed, watch } from 'vue';
import { darkTheme, createDiscreteApi } from 'naive-ui';

import { MaxTimeout } from '@/config';
import { preprocessInput } from '@/utils';
import { useSentry } from '@/hooks/useZsentry';
import { writeBufferToTerminal } from '@/utils';
import { Sentry } from 'nora-zmodemjs/src/zmodem_browser';
import { useTerminalSettingsStore } from '@/store/modules/terminalSettings';
import { sendEventToLuna, updateIcon } from '@/components/TerminalComponent/helper';
import { useConnectionStore } from '@/store/modules/useConnection';

import type { ConfigProviderProps } from 'naive-ui';
import type { SettingConfig } from '@/hooks/interface';
import type { OnlineUser } from '@/types/modules/user.type';
import type { ShareUserOptions } from '@/types/modules/user.type';
import { FormatterMessageType, MessageType, SendLunaMessageType, ZmodemActionType } from '@/enum';

/**
 * @description 格式化消息
 * @param id
 * @param type
 * @param data
 * @returns
 */
export const formatMessage = (id: string, type: FormatterMessageType, data: any) => {
  return JSON.stringify({
    id,
    type,
    data
  });
};

export const useTerminalConnection = (lunaId: string, origin: string) => {
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

      let currentDate = new Date();

      if (lastReceiveTime.value.getTime() - currentDate.getTime() > MaxTimeout) {
        socket.close();
      }

      let pingTimeout: number = currentDate.getTime() - lastSendTime.value.getTime();

      if (pingTimeout < 0) return;

      socket.send(formatMessage('', FormatterMessageType.PING, ''));
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

    let parsedMessageData = JSON.parse(data);

    switch (parsedMessageData.type) {
      case MessageType.CLOSE: {
        enableShare.value = false;
        onlineUsers.value = [];

        connectionStore.updateConnectionState(terminalId.value, {
          enableShare: false,
          onlineUsers: []
        });

        socket.close();

        sendEventToLuna(SendLunaMessageType.CLOSE, '', lunaId, origin);
        break;
      }
      case MessageType.ERROR: {
        terminal.write(parsedMessageData.err);

        sendEventToLuna(SendLunaMessageType.TERMINAL_ERROR, '', lunaId, origin);
        break;
      }
      case MessageType.PING: {
        break;
      }
      case MessageType.CONNECT: {
        terminalId.value = parsedMessageData.id;
        connectionStore.setConnectionState(terminalId.value, {
          lunaId: lunaId,
          origin: origin,
          socket: socket,
          terminal: terminal
        });

        const terminalData = {
          cols: terminal.cols,
          rows: terminal.rows,
          code: shareCode.value
        };

        const info = JSON.parse(parsedMessageData.data);

        featureSetting.value = info.setting;

        updateIcon(info.setting);

        socket.send(formatMessage(terminalId.value, FormatterMessageType.TERMINAL_INIT, JSON.stringify(terminalData)));
        break;
      }
      case MessageType.MESSAGE_NOTIFY: {
        break;
      }
      case MessageType.TERMINAL_SHARE: {
        const data = JSON.parse(parsedMessageData.data);

        shareId.value = data.share_id;
        shareCode.value = data.code;
        connectionStore.updateConnectionState(terminalId.value, {
          shareId: data.share_id,
          shareCode: data.code
        });

        break;
      }
      case MessageType.TERMINAL_ACTION: {
        const actionType = parsedMessageData.data;

        switch (actionType) {
          case ZmodemActionType.ZMODEM_START: {
            zmodemTransferStatus.value = true;
            break;
          }
          case ZmodemActionType.ZMODEM_END: {
            terminal.write('\r\n');
            break;
          }
          default: {
            zmodemTransferStatus.value = false;
          }
        }
        break;
      }
      case MessageType.TERMINAL_SESSION: {
        const sessionInfo = JSON.parse(parsedMessageData.data);
        const sessionDetail = sessionInfo.session;

        const share = sessionInfo.permission.actions.includes('share');

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
        }

        sessionId.value = sessionDetail.id;
        terminalSettingsStore.setDefaultTerminalConfig('theme', sessionInfo.themeName);

        break;
      }
      case MessageType.TERMINAL_SHARE_JOIN: {
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
      case MessageType.TERMINAL_PERM_VALID: {
        clearInterval(warningInterval.value!);
        message.info(`${t('PermissionValid')}`);
        break;
      }
      case MessageType.TERMINAL_SHARE_LEAVE: {
        const data: OnlineUser = JSON.parse(parsedMessageData.data);

        const index = onlineUsers.value.findIndex(item => item.user_id === data.user_id);

        if (index !== -1) {
          onlineUsers.value.splice(index, 1);

          connectionStore.updateConnectionState(terminalId.value, {
            onlineUsers: onlineUsers.value
          });

          message.info(`${data.user} ${t('LeaveShare')}`);
        }
        break;
      }
      case MessageType.TERMINAL_PERM_EXPIRED: {
        const data = JSON.parse(parsedMessageData.data);
        const warningMsg = `${t('PermissionExpired')}: ${data.detail}`;

        message.warning(warningMsg);

        warningInterval.value = setInterval(() => {
          message.warning(warningMsg);
        }, 1000 * 60);
        break;
      }
      case MessageType.TERMINAL_SESSION_PAUSE: {
        const data = JSON.parse(parsedMessageData.data);

        message.info(`${data.user} ${t('PauseSession')}`);
        break;
      }
      case MessageType.TERMINAL_GET_SHARE_USER: {
        userOptions.value = JSON.parse(parsedMessageData.data);

        connectionStore.updateConnectionState(terminalId.value, {
          userOptions: userOptions.value
        });

        break;
      }
      case MessageType.TERMINAL_SESSION_RESUME: {
        const data = JSON.parse(parsedMessageData.data);

        message.info(`${data.user} ${t('ResumeSession')}`);
        break;
      }
      case MessageType.TERMINAL_SHARE_USER_REMOVE: {
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
   * @description 创建分享链接
   */
  const handleCreateShareUrl = (
    socket: WebSocket,
    shareLinkRequest: any
  ): Promise<{ shareId: string; shareCode: string }> => {
    if (!socket || !terminalId.value) return Promise.reject('');

    return new Promise(resolve => {
      const origin = window.location.origin;

      socket?.send(
        formatMessage(
          terminalId.value,
          FormatterMessageType.TERMINAL_SHARE,
          JSON.stringify({
            origin,
            session: sessionId.value,
            users: shareLinkRequest.users,
            expired_time: shareLinkRequest.expiredTime,
            action_permission: shareLinkRequest.actionPerm
          })
        )
      );

      watch(
        [shareId, shareCode],
        ([newShareId, newShareCode]) => {
          if (newShareId && newShareCode) {
            resolve({
              shareId: newShareId,
              shareCode: newShareCode
            });
          }
        },
        { immediate: true }
      );
    });
  };
  /**
   * @description 移除指定分享用户
   * @param socket
   */
  const handeleRemoveShareUser = (socket: WebSocket, userMeta: OnlineUser) => {
    socket.send(
      formatMessage(
        terminalId.value,
        FormatterMessageType.TERMINAL_SHARE_USER_REMOVE,
        JSON.stringify({
          session: sessionId.value,
          user_meta: userMeta
        })
      )
    );
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
        formatMessage(terminalId.value, FormatterMessageType.TERMINAL_GET_SHARE_USER, JSON.stringify({ query }))
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
      terminal.write('\x1b[31mConnection Websocket Has Been Closed\x1b[0m');
    };
    socket.onerror = () => {
      terminal.write('\x1b[31mConnection Websocket Error Occurred\x1b[0m');
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
      let processedData = preprocessInput(data, terminalSettingsStore.getConfig);

      lastSendTime.value = new Date();

      sendEventToLuna('KEYBOARDEVENT', '');

      socket.send(formatMessage(terminalId.value, FormatterMessageType.TERMINAL_DATA, processedData));
    });
  };

  return {
    connectionStatus: {
      sessionId,
      terminalId,
      enableShare,
      userOptions,
      onlineUsers
    },
    getShareUser,
    setShareCode,
    handleCreateShareUrl,
    initializeSocketEvent,
    handeleRemoveShareUser
  };
};
