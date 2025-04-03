import { useI18n } from 'vue-i18n';
import { Terminal } from '@xterm/xterm';
import { ref, computed, nextTick } from 'vue';
import { darkTheme, createDiscreteApi } from 'naive-ui';

import { MaxTimeout } from '@/config';
import { preprocessInput } from '@/utils';
import { useSentry } from '@/hooks/useZsentry';
import { writeBufferToTerminal } from '@/utils';
import { Sentry } from 'nora-zmodemjs/src/zmodem_browser';
import { useTerminalSettingsStore } from '@/store/modules/terminalSettings';
import { sendEventToLuna, updateIcon } from '@/components/TerminalComponent/helper';

import type { ConfigProviderProps } from 'naive-ui';

export enum FormatterMessageType {
  PING = 'PING',
  TERMINAL_INIT = 'TERMINAL_INIT',
  TERMINAL_DATA = 'TERMINAL_DATA',
  TERMINAL_RESIZE = 'TERMINAL_RESIZE',
  TERMINAL_K8S_DATA = 'TERMINAL_K8S_DATA',
  TERMINAL_K8S_RESIZE = 'TERMINAL_K8S_RESIZE'
}

enum SendLunaMessageType {
  PING = 'PING',
  CLOSE = 'CLOSE',
  PONG = 'PONG',
  CONNECT = 'CONNECT',
  TERMINAL_ERROR = 'TERMINAL_ERROR',
  MESSAGE_NOTIFY = 'MESSAGE_NOTIFY'
}

enum ZmodemActionType {
  ZMODEM_START = 'ZMODEM_START',
  ZMODEM_END = 'ZMODEM_END'
}

enum MessageType {
  PING = 'PING',
  CLOSE = 'CLOSE',
  ERROR = 'ERROR',
  CONNECT = 'CONNECT',
  TERMINAL_ERROR = 'TERMINAL_ERROR',
  MESSAGE_NOTIFY = 'MESSAGE_NOTIFY',
  TERMINAL_ACTION = 'TERMINAL_ACTION',
  TERMINAL_SHARE_USER_REMOVE = 'TERMINAL_SHARE_USER_REMOVE'
}

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

  let terminalId = ref<string>('');
  let zmodemTransferStatus = ref<boolean>(true);
  let pingInterval = ref<number | null>(null);

  const lastSendTime = ref(new Date());
  const lastReceiveTime = ref(new Date());

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
   * @description 分发消息
   * @param data
   * @param terminal
   * @param socket
   */
  const dispatch = (data: string, terminal: Terminal, socket: WebSocket) => {
    if (!data) return;

    let parsedMessageData = JSON.parse(data);

    switch (parsedMessageData.type) {
      case MessageType.CONNECT: {
        terminalId.value = parsedMessageData.id;

        const terminalData = {
          cols: terminal.cols,
          rows: terminal.rows,
          code: ''
        };

        const info = JSON.parse(parsedMessageData.data);

        // todo 设置 setting 和 当前用户

        // 更新网页图标
        updateIcon(info.setting);

        socket.send(formatMessage(terminalId.value, FormatterMessageType.TERMINAL_INIT, JSON.stringify(terminalData)));
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
      case MessageType.CLOSE: {
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
      case MessageType.MESSAGE_NOTIFY: {
        break;
      }
      case MessageType.TERMINAL_SHARE_USER_REMOVE: {
        // todo
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
   * @description 初始化 socket 事件
   * @param terminal
   * @param socket
   */
  const initializeSocketEvent = (terminal: Terminal, socket: WebSocket) => {
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
        dispatch(event.data, terminal, socket);
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
    terminalId,
    initializeSocketEvent
  };
};
