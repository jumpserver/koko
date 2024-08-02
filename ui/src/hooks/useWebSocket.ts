import ZmodemBrowser from 'nora-zmodemjs/src/zmodem_browser';

import { Ref } from 'vue';
import { useRoute } from 'vue-router';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { useLogger } from '@/hooks/useLogger.ts';
import { createDiscreteApi } from 'naive-ui';
import { BASE_WS_URL, MaxTimeout } from '@/config';
import { fireEvent, writeBufferToTerminal } from '@/utils';
import type { SettingConfig } from '@/hooks/interface';

import {
  formatMessage,
  handleError,
  sendEventToLuna,
  updateIcon
} from '@/components/Terminal/helper';

const { message } = createDiscreteApi(['message']);

const { debug } = useLogger('useWebSocket');

/**
 * 管理 WebSocket 连接的自定义 hook，支持 Zmodem 文件传输。
 *
 * @param {boolean} enableZmodem - 是否启用 Zmodem。
 * @param {Ref<boolean>} zmodemStatus - 跟踪 Zmodem 状态的 Ref。
 * @param {Ref<ZmodemBrowser.Sentry | null>} sentryRef - Zmodem Sentry 实例的 Ref。
 * @param {FitAddon} fitAddon - xterm.js 的 FitAddon 实例。
 * @param {any} shareCode - 终端会话的分享代码。
 * @param {Ref<any>} currentUser - 跟踪当前用户信息的 Ref。
 * @param {Function} emits - 用于向父组件发出事件的函数。
 * @returns {Object} - WebSocket 实用函数。
 */
export const useWebSocket = (
  enableZmodem: boolean,
  zmodemStatus: Ref<boolean>,
  sentryRef: Ref<ZmodemBrowser.Sentry | null>,
  fitAddon: FitAddon,
  shareCode: any,
  currentUser: Ref<any>,
  emits: (
    event: 'wsData',
    msgType: string,
    msg: any,
    terminal: Terminal,
    setting: SettingConfig
  ) => void
): any => {
  let ws: WebSocket;
  let terminal: Terminal;
  let lastReceiveTime: Date;
  let id: Ref<string>;

  let pingInterval: number;

  let setting: SettingConfig;

  let lastSendTime: Ref<Date>;

  /**
   * 处理 WebSocket 消息。
   *
   * @param {MessageEvent} e - WebSocket 消息事件。
   */
  const handleMessage = (e: MessageEvent) => {
    lastSendTime.value = new Date();

    if (typeof e.data === 'object') {
      if (enableZmodem) {
        sentryRef.value && sentryRef.value.consume(e.data);
      } else {
        writeBufferToTerminal(enableZmodem, zmodemStatus.value, terminal, e.data);
      }
    } else {
      debug(typeof e.data);
      dispatch(e.data);
    }
  };

  /**
   * 处理 WebSocket 连接打开事件。
   *
   * @param {string} terminalId - 终端的 ID。
   * @param {WebSocket} socket - WebSocket 实例。
   */
  const onWebsocketOpen = (terminalId: string, socket: WebSocket) => {
    sendEventToLuna('CONNECTED', '');

    if (pingInterval !== null) clearInterval(pingInterval);

    lastReceiveTime = new Date();

    pingInterval = setInterval(() => {
      if (socket.readyState === WebSocket.CLOSING || socket.readyState === WebSocket.CLOSED)
        return clearInterval(pingInterval);

      let currentDate: Date = new Date();

      if (lastReceiveTime.getTime() - currentDate.getTime() > MaxTimeout) {
        debug('More than 30s do not receive data');
      }

      let pingTimeout: number = currentDate.getTime() - lastSendTime.value.getTime() - MaxTimeout;

      if (pingTimeout < 0) return;

      socket.send(formatMessage(terminalId, 'PING', ''));
    }, 25 * 1000);
  };

  /**
   * 分派 WebSocket 消息到相应的处理程序。
   *
   * @param {any} data - WebSocket 消息数据。
   */
  const dispatch = (data: any) => {
    if (data === undefined) return;

    let msg = JSON.parse(data);

    switch (msg.type) {
      case 'CONNECT': {
        id.value = msg.id;

        try {
          fitAddon.fit();
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

        ws.send(formatMessage(id.value, 'TERMINAL_INIT', JSON.stringify(terminalData)));
        break;
      }
      case 'CLOSE': {
        terminal.writeln('Receive Connection closed');
        ws.close();
        sendEventToLuna('CLOSE', '');
        break;
      }
      case 'PING':
        break;
      case 'TERMINAL_ACTION':
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

  /**
   * 根据当前路由生成 WebSocket URL。
   *
   * @returns {string} - 生成的 WebSocket URL。
   */
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

  /**
   * 创建一个新的 WebSocket 连接。
   *
   * @param  term - xterm.js Terminal 实例。
   * @param  lastSend - 跟踪最后发送时间的 Ref。
   * @param  terminalId - 跟踪终端 ID 的 Ref。
   * @returns {WebSocket} - WebSocket 实例。
   */
  const createWebSocket = (
    term: Terminal,
    lastSend: Ref<Date>,
    terminalId: Ref<string>
  ): WebSocket => {
    id = terminalId;
    terminal = term;
    lastSendTime = lastSend;

    const connectURL = generateWsURL();

    const socket = new WebSocket(connectURL, ['JMS-KOKO']);

    socket.onopen = () => onWebsocketOpen(terminalId.value, socket);
    socket.onmessage = (e: MessageEvent) => handleMessage(e);
    socket.onerror = e => {
      terminal.write('Connection Websocket Error');
      fireEvent(new Event('CLOSE', {}));
      handleError(e);
    };
    socket.onclose = e => {
      terminal.write('Connection WebSocket Closed');
      fireEvent(new Event('CLOSE', {}));
      handleError(e);
    };

    ws = socket;

    return socket;
  };

  return {
    createWebSocket,
    handleMessage
  };
};
