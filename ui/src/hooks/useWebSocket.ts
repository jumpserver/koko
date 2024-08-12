// 引入 hook
import { useRoute } from 'vue-router';
import { createDiscreteApi } from 'naive-ui';
import { useTree } from '@/hooks/useTree.ts';
import { useLogger } from '@/hooks/useLogger.ts';
import { useSentry } from '@/hooks/useZsentry.ts';
import { useTreeStore } from '@/store/modules/tree.ts';
import { useParamsStore } from '@/store/modules/params.ts';
import { useWebSocket as useVueuseWebSocket } from '@vueuse/core';

// 引入类型定义
import { Ref } from 'vue';
import type { WebSocketStatus } from '@vueuse/core';
import type { paramsOptions } from '@/hooks/interface';
import type { Sentry } from 'nora-zmodemjs/src/zmodem_browser';

import { ref } from 'vue';
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
 * @param options 配置项
 * @returns WebSocket
 */
export const useWebSocket = (terminalId: Ref<string>, options: paramsOptions): any => {
  let _sentry: Sentry;
  let _terminal: Terminal | undefined;
  let _fitAddon: FitAddon | undefined;

  let _pingInterval: number;

  let _lastReceiveTime: Date;

  let _lastSendTime: Ref<Date> = ref(new Date());

  // 处理 WebSocket 消息
  const handleMessage = (
    data: Ref<any>,
    close: WebSocket['close'],
    send: (data: string | ArrayBuffer | Blob, useBuffer?: boolean) => boolean
  ) => {
    _lastSendTime.value = new Date();

    if (typeof data.value === 'object') {
      if (options.enableZmodem) {
        _sentry.consume(data.value);
      } else {
        writeBufferToTerminal(
          options.enableZmodem,
          options.zmodemStatus?.value ?? false,
          _terminal ? _terminal : null,
          data.value
        );
      }
    } else {
      debug(typeof data.value);
      dispatch(data.value, close, send);
    }
  };

  // 处理 k8s 消息
  const handleK8sMessage = (
    data: Ref<any>,
    close: WebSocket['close'],
    ws: WebSocket,
    send: (data: string | ArrayBuffer | Blob, useBuffer?: boolean) => boolean
  ) => {
    const treeStore = useTreeStore();
    const { initTree, updateTreeNodes } = useTree(ws, send);

    _lastSendTime.value = new Date();

    if (data.value === undefined) return;

    let msg = JSON.parse(data.value);

    switch (msg.type) {
      case 'CONNECT': {
        terminalId.value = msg.id;

        const info = JSON.parse(msg.data);

        // 初始化 Tree
        treeStore.setConnectInfo(msg.id, info, initTree);

        debug('K8s Websocket Connection Established');
        break;
      }
      case 'TERMINAL_K8S_TREE': {
        updateTreeNodes(msg);
        break;
      }
      case 'PING': {
        break;
      }
      case 'CLOSE':
      case 'ERROR': {
        message.error('Receive Connection Closed');
        close();
        break;
      }
      default: {
      }
    }
  };

  // 处理 WebSocket 连接打开事件
  const onWebsocketOpen = (
    status: Ref<WebSocketStatus>,
    send: (data: string | ArrayBuffer | Blob, useBuffer?: boolean) => boolean
  ) => {
    sendEventToLuna('CONNECTED', '');

    if (_pingInterval !== null) clearInterval(_pingInterval);

    _lastReceiveTime = new Date();

    _pingInterval = setInterval(() => {
      if (status.value === 'CLOSED') return clearInterval(_pingInterval);

      let currentDate: Date = new Date();

      if (_lastReceiveTime.getTime() - currentDate.getTime() > MaxTimeout) {
        debug('More than 30s do not receive data');
      }

      let pingTimeout: number = currentDate.getTime() - _lastSendTime.value.getTime() - MaxTimeout;

      if (pingTimeout < 0) return;

      send(formatMessage(terminalId.value, 'PING', ''));
    }, 25 * 1000);
  };

  // 分派 WebSocket 消息到相应的处理程序
  const dispatch = (
    data: any,
    close: WebSocket['close'],
    send: (data: string | ArrayBuffer | Blob, useBuffer?: boolean) => boolean
  ) => {
    if (data === undefined) return;

    let msg = JSON.parse(data);
    const paramsStore = useParamsStore();

    switch (msg.type) {
      case 'CONNECT': {
        terminalId.value = msg.id;

        try {
          _fitAddon && _fitAddon.fit();
        } catch (e) {
          console.log(e);
        }

        const terminalData = {
          cols: _terminal && _terminal.cols,
          rows: _terminal && _terminal.rows,
          code: paramsStore.shareCode
        };

        const info = JSON.parse(msg.data);
        debug('dispatchInfo', info);

        paramsStore.setSetting(info.setting);
        paramsStore.setCurrentUser(info.user);

        updateIcon(info.setting);

        send(formatMessage(terminalId.value, 'TERMINAL_INIT', JSON.stringify(terminalData)));
        break;
      }
      case 'CLOSE': {
        _terminal && _terminal.writeln('Receive Connection closed');
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
            options.zmodemStatus.value = true;

            if (!options.enableZmodem) {
              options.i18nCallBack && message.info(options.i18nCallBack('WaitFileTransfer'));
            }
            break;
          }
          case 'ZMODEM_END': {
            if (!options.enableZmodem && options.zmodemStatus?.value) {
              options.i18nCallBack && message.info(options.i18nCallBack('EndFileTransfer'));

              _terminal && _terminal.write('\r\n');

              options.zmodemStatus.value = false;
            }
            break;
          }
          default: {
            options.zmodemStatus.value = false;
          }
        }
        break;
      case 'TERMINAL_ERROR':
      case 'ERROR': {
        message.error(msg.err);
        _terminal && _terminal.writeln(msg.err);
        break;
      }
      case 'MESSAGE_NOTIFY': {
        break;
      }
      default: {
        debug(`Default: ${data}`);
      }
    }

    if (options.emitCallback && _terminal) {
      options.emitCallback(msg.type, msg, _terminal);
    }
  };

  // 根据当前路由生成 WebSocket URL
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
        requireParams.append('target_id', params.id ? params.id.toString() : '');

        connectURL = BASE_WS_URL + '/koko/ws/token/?' + requireParams.toString();
        break;
      }
      case 'TokenParams': {
        connectURL = urlParams ? `${BASE_WS_URL}/koko/ws/token/?${urlParams.toString()}` : '';
        break;
      }
      case 'kubernetes': {
        connectURL = `${BASE_WS_URL}/koko/ws/terminal/?token=${route.query.token}`;
        break;
      }
      default: {
        connectURL = urlParams ? `${BASE_WS_URL}/koko/ws/terminal/?${urlParams.toString()}` : '';
      }
    }

    if (!connectURL) {
      throw new Error('Unable to generate WebSocket URL, missing parameters.');
    }

    return connectURL;
  };

  // 创建 WebSocket
  const generateWebSocket = (connectURL: string) => {
    const { status, data, send, open, close, ws } = useVueuseWebSocket(connectURL, {
      onConnected: () => {
        onWebsocketOpen(status, send);
        ws.value && (ws.value.binaryType = 'arraybuffer');
      },
      onMessage: (_ws: WebSocket, _event: MessageEvent<any>) => {
        if (!options.isK8s) {
          handleMessage(data, close, send);
        } else {
          handleK8sMessage(data, close, _ws, send);
        }
      },
      onError: (_ws: WebSocket, event: Event) => {
        _terminal && _terminal.write('Connection Websocket Error');
        fireEvent(new Event('CLOSE', {}));
        handleError(event);
      },
      onDisconnected: (_ws: WebSocket, event: CloseEvent) => {
        _terminal && _terminal.write('Connection WebSocket Closed');
        fireEvent(new Event('CLOSE', {}));
        handleError(event);
      },
      protocols: ['JMS-KOKO']
    });

    return { status, data, send, open, close, ws };
  };

  const createWebSocket = (
    lastSendTime: Ref<Date>,
    fitAddon?: FitAddon,
    term?: Terminal,
    t?: any
  ) => {
    _terminal = term;
    _fitAddon = fitAddon;
    _lastSendTime = lastSendTime;

    const { createSentry } = useSentry(_lastSendTime, t);

    const connectURL: string = generateWsURL();

    const { status, data, send, open, close, ws } = generateWebSocket(connectURL);

    if (ws && term) {
      _sentry = createSentry(<WebSocket>ws.value, term);
    }

    return { status, data, send, open, close, ws };
  };

  return {
    createWebSocket,
    onWebsocketOpen
  };
};
