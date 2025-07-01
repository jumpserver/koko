import type { Terminal } from '@xterm/xterm';
import type { ConfigProviderProps } from 'naive-ui';
import type { Sentry } from 'nora-zmodemjs/src/zmodem_browser';

import mitt from 'mitt';
import { useI18n } from 'vue-i18n';
import { computed, ref } from 'vue';
import { createDiscreteApi, darkTheme } from 'naive-ui';

import type { SettingConfig } from '@/types/modules/config.type';
import type { TerminalSessionInfo } from '@/types/modules/postmessage.type';
import type { OnlineUser, ShareUserOptions } from '@/types/modules/user.type';

import { updateIcon } from '@/hooks/helper';
import { MaxTimeout } from '@/utils/config';
import { useSentry } from '@/hooks/useZsentry';
import { useConnectionStore } from '@/store/modules/useConnection';
import { useTerminalSettingsStore } from '@/store/modules/terminalSettings';
import { formatMessage, preprocessInput, writeBufferToTerminal } from '@/utils';
import {
  FORMATTER_MESSAGE_TYPE,
  LUNA_MESSAGE_TYPE,
  MESSAGE_TYPE,
  ZMODEM_ACTION_TYPE,
} from '@/types/modules/message.type';

export const eventBus = mitt<{
  'luna-event': { event: string; data: string };
  'terminal-session': TerminalSessionInfo;
  'terminal-connect': { id: string };
}>();
// 修改 sendEventToLuna 函数
export function sendLunaEvent(event: string, data: string) {
  eventBus.emit('luna-event', { event, data });
}

export function useTerminalConnection() {
  let sentry: Sentry;

  const onlineUsers = ref<OnlineUser[]>([]);

  const shareId = ref<string>('');
  const shareCode = ref<string>('');
  const sessionId = ref<string>('');
  const terminalId = ref<string>('');
  const pingInterval = ref<ReturnType<typeof setInterval> | null>(null);
  const warningInterval = ref<ReturnType<typeof setInterval> | null>(null);
  const enableShare = ref<boolean>(false);

  const zmodemTransferStatus = ref<boolean>(true);

  const lastSendTime = ref(new Date());
  const lastReceiveTime = ref(new Date());
  const userOptions = ref<ShareUserOptions[]>([]);
  const featureSetting = ref<Partial<SettingConfig>>({});

  const connectionStore = useConnectionStore();
  const terminalSettingsStore = useTerminalSettingsStore();

  const configProviderPropsRef = computed<ConfigProviderProps>(() => ({
    theme: darkTheme,
  }));

  const { message } = createDiscreteApi(['message'], {
    configProviderProps: configProviderPropsRef,
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
      const pongTimeout = currentDate.getTime() - lastReceiveTime.value.getTime() - MaxTimeout;
      const pingTimeout: number = currentDate.getTime() - lastSendTime.value.getTime() - MaxTimeout;

      if (pingTimeout < 0 && pongTimeout < 0) return;

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

        connectionStore.updateConnectionState({
          enableShare: false,
          onlineUsers: [],
        });

        socket.close();
        sendLunaEvent(LUNA_MESSAGE_TYPE.CLOSE, '');
        break;
      }
      case MESSAGE_TYPE.ERROR: {
        terminal.write(parsedMessageData.err);
        sendLunaEvent(LUNA_MESSAGE_TYPE.TERMINAL_ERROR, '');
        break;
      }
      case MESSAGE_TYPE.PING: {
        break;
      }
      case MESSAGE_TYPE.CONNECT: {
        terminalId.value = parsedMessageData.id;
        eventBus.emit('terminal-connect', { id: terminalId.value });

        connectionStore.setConnectionState({
          socket,
          terminal,
          terminalId: parsedMessageData.id,
        });

        const terminalData = {
          cols: terminal.cols,
          rows: terminal.rows,
          code: shareCode.value,
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
          console.error(e);
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

    connectionStore.updateConnectionState({
      shareCode: code,
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
    const isClosed = computed(() => socket.readyState === WebSocket.CLOSED || socket.readyState === WebSocket.CLOSING);

    socket.onopen = () => {
      socket.binaryType = 'arraybuffer';
      heartBeat(socket);
    };
    socket.onclose = () => {
      terminal.write('\r\n');
      terminal.write('\r\n');
      terminal.write('\x1B[31mConnection websocket has been closed\x1B[0m');
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

      if (isClosed.value) {
        return;
      }
      socket.send(formatMessage(terminalId.value, FORMATTER_MESSAGE_TYPE.TERMINAL_DATA, processedData));
    });
  };

  return {
    setShareCode,
    initializeSocketEvent,
    eventBus,
  };
}
