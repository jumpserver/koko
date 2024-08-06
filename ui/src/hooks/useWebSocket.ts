// 引入 hook
import { useRoute } from 'vue-router';
import { createDiscreteApi } from 'naive-ui';
import { useLogger } from '@/hooks/useLogger.ts';
import { useSentry } from '@/hooks/useZsentry.ts';
import { useWebSocket as useVueuseWebSocket } from '@vueuse/core';

// 引入类型定义
import type { WebSocketStatus } from '@vueuse/core';
import type { SettingConfig } from '@/hooks/interface';
import type { Sentry } from 'nora-zmodemjs/src/zmodem_browser';

import { ref, Ref } from 'vue';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { BASE_WS_URL, MaxTimeout } from '@/config';
import { fireEvent, writeBufferToTerminal } from '@/utils';

// 引入工具函数
import {
  updateIcon,
  handleError,
  formatMessage,
  sendEventToLuna
} from '@/components/Terminal/helper';

const { debug } = useLogger('useWebSocket');
const { message } = createDiscreteApi(['message']);

/**
 * 管理 WebSocket 连接的自定义 hook，支持 Zmodem 文件传输。
 *
 * @param terminalId
 * @param enableZmodem - 是否启用 Zmodem。
 * @param zmodemStatus - 跟踪 Zmodem 状态的 Ref。
 * @param shareCode - 终端会话的分享代码。
 * @param currentUser - 跟踪当前用户信息的 Ref。
 * @param emits - 用于向父组件发出事件的函数。
 * @param t
 * @returns WebSocket
 */
export const useWebSocket = (
  terminalId: Ref<string>,
  enableZmodem: boolean,
  zmodemStatus: Ref<boolean>,
  shareCode: any,
  currentUser: Ref<any>,
  emits: (
    event: 'wsData',
    msgType: string,
    msg: any,
    terminal: Terminal,
    setting: SettingConfig
  ) => void,
  t: any
): any => {
  let setting: SettingConfig;
  let terminal: Terminal;
  let lastReceiveTime: Date;
  let pingInterval: number;
  let lastSendTime: Ref<Date> = ref(new Date());

  let sentry: Sentry;
  let _fitAddon: Ref<FitAddon> = ref(new FitAddon());

  /* 处理 WebSocket 消息 */
  const handleMessage = (
    data: Ref<any>,
    close: WebSocket['close'],
    send: (data: string | ArrayBuffer | Blob, useBuffer?: boolean) => boolean
  ) => {
    lastSendTime.value = new Date();

    if (typeof data.value === 'object') {
      if (enableZmodem) {
        sentry.consume(data.value);
      } else {
        writeBufferToTerminal(enableZmodem, zmodemStatus.value, terminal, data.value);
      }
    } else {
      debug(typeof data.value);
      dispatch(data.value, close, send);
    }
  };

  /* 处理 WebSocket 连接打开事件 */
  const onWebsocketOpen = (
    status: Ref<WebSocketStatus>,
    send: (data: string | ArrayBuffer | Blob, useBuffer?: boolean) => boolean
  ) => {
    sendEventToLuna('CONNECTED', '');

    if (pingInterval !== null) clearInterval(pingInterval);

    lastReceiveTime = new Date();

    pingInterval = setInterval(() => {
      if (status.value === 'CLOSED') return clearInterval(pingInterval);

      let currentDate: Date = new Date();

      if (lastReceiveTime.getTime() - currentDate.getTime() > MaxTimeout) {
        debug('More than 30s do not receive data');
      }

      let pingTimeout: number = currentDate.getTime() - lastSendTime.value.getTime() - MaxTimeout;

      if (pingTimeout < 0) return;

      send(formatMessage(terminalId.value, 'PING', ''));
    }, 25 * 1000);
  };

  /* 分派 WebSocket 消息到相应的处理程序 */
  const dispatch = (
    data: any,
    close: WebSocket['close'],
    send: (data: string | ArrayBuffer | Blob, useBuffer?: boolean) => boolean
  ) => {
    if (data === undefined) return;

    let msg = JSON.parse(data);

    switch (msg.type) {
      case 'CONNECT': {
        terminalId.value = msg.id;

        try {
          _fitAddon.value?.fit();
        } catch (e) {
          console.log(e);
        }

        const terminalData = {
          cols: terminal.cols,
          rows: terminal.rows,
          code: shareCode
        };

        const info = JSON.parse(msg.data);
        debug('dispatchInfo', info);

        currentUser.value = info.user;

        setting = info.setting;

        updateIcon(setting);

        send(formatMessage(terminalId.value, 'TERMINAL_INIT', JSON.stringify(terminalData)));
        break;
      }
      case 'CLOSE': {
        terminal.writeln('Receive Connection closed');
        close();
        sendEventToLuna('CLOSE', '');
        break;
      }
      case 'PING':
        break;
      case 'TERMINAL_ACTION':
        const action = msg.data;

        switch (action) {
          case 'ZMODEM_START': {
            zmodemStatus.value = true;

            if (!enableZmodem) {
              message.info(t('WaitFileTransfer'));
            }
            break;
          }
          case 'ZMODEM_END': {
            if (!enableZmodem && zmodemStatus.value) {
              message.info(t('EndFileTransfer'));
              terminal.write('\r\n');

              zmodemStatus.value = false;
            }
            break;
          }
          default: {
            zmodemStatus.value = false;
          }
        }
        break;
      case 'TERMINAL_ERROR':
      case 'ERROR': {
        message.error(msg.err);
        terminal.writeln(msg.err);
        break;
      }
      case 'MESSAGE_NOTIFY': {
        break;
      }
      default: {
        debug(`Default: ${data}`);
      }
    }

    emits('wsData', msg.type, msg, terminal, setting);
  };

  /* 根据当前路由生成 WebSocket URL */
  const generateWsURL = (): string => {
    const route = useRoute();

    const routeName = route.name;
    const urlParams = new URLSearchParams(window.location.search.slice(1));

    let connectURL;

    switch (routeName) {
      case 'Token': {
        const params = route.params;
        const requireParams = new URLSearchParams();

        requireParams.append('type', 'token');
        requireParams.append('target_id', params.id as string);

        connectURL = BASE_WS_URL + '/koko/ws/token/?' + requireParams.toString();
        break;
      }
      case 'TokenParams': {
        connectURL = urlParams && `${BASE_WS_URL}/koko/ws/token/?${urlParams.toString()}`;
        break;
      }
      default: {
        connectURL = urlParams && `${BASE_WS_URL}/koko/ws/terminal/?${urlParams.toString()}`;
      }
    }

    return connectURL;
  };

  /* 创建 WebSocket */
  const generateWebSocket = (connectURL: string) => {
    const { status, data, send, open, close, ws } = useVueuseWebSocket(connectURL, {
      onConnected: () => {
        onWebsocketOpen(status, send);
        ws.value && (ws.value.binaryType = 'arraybuffer');
      },
      onMessage: (_ws: WebSocket, _event: MessageEvent<any>) => {
        handleMessage(data, close, send);
      },
      onError: (_ws: WebSocket, event: Event) => {
        terminal.write('Connection Websocket Error');
        fireEvent(new Event('CLOSE', {}));
        handleError(event);
      },
      onDisconnected: (_ws: WebSocket, event: CloseEvent) => {
        terminal.write('Connection WebSocket Closed');
        fireEvent(new Event('CLOSE', {}));
        handleError(event);
      },
      protocols: ['JMS-KOKO']
    });

    /**
     * status: 当前 WebSocket 连接的状态
     * data: 从 WebSocket 接收到的最新消息
     * send: 通过 WebSocket 发送消息
     * open: 用于手动打开 WebSocket 连接
     * close: 手动关闭 WebSocket 连接
     * ws: 当前 WebSocket 实例
     */
    return { status, data, send, open, close, ws };
  };

  const createWebSocket = (
    term: Terminal,
    lastSend: Ref<Date>,
    fitAddon: FitAddon
  ): {
    data: Ref<any>;
    ws: Ref<WebSocket | undefined>;
    send: (data: string | ArrayBuffer | Blob, useBuffer?: boolean) => boolean;
    close: { (code?: number, reason?: string): void; (code?: number, reason?: string): void };
    open: () => void;
    status: Ref<WebSocketStatus>;
  } => {
    terminal = term;
    _fitAddon.value = fitAddon;
    lastSendTime = lastSend;

    const { createSentry } = useSentry(lastSendTime, t);

    const connectURL = generateWsURL();

    const { status, data, send, open, close, ws } = generateWebSocket(connectURL);

    ws && (sentry = createSentry(<WebSocket>ws.value, term));

    return { status, data, send, open, close, ws };
  };

  return {
    createWebSocket
  };
};
