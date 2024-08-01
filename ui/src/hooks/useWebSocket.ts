import { Ref } from 'vue';
import { Terminal } from '@xterm/xterm';
import { fireEvent, writeBufferToTerminal } from '@/utils';
import { useLogger } from '@/hooks/useLogger.ts';
import { MaxTimeout } from '@/config';
import {
  formatMessage,
  handleError,
  sendEventToLuna,
  updateIcon
} from '@/components/Terminal/helper';
import { createDiscreteApi } from 'naive-ui';
import ZmodemBrowser from 'nora-zmodemjs/src/zmodem_browser';
import { FitAddon } from '@xterm/addon-fit';

const { message } = createDiscreteApi(['message']);

const { debug } = useLogger('useWebSocket');

export const useWebSocket = () => {
  let ws: WebSocket;
  let terminal: Terminal;
  let lastReceiveTime: Date;

  let pingInterval: number;

  let lastSendTime: Ref<Date>;

  /**
   * @description 在 WebSocket 连接成功建立时触发的回调
   */
  const onWebsocketOpen = (terminalId: string) => {
    sendEventToLuna('CONNECTED', '');

    if (pingInterval !== null) clearInterval(pingInterval);

    lastReceiveTime = new Date();

    pingInterval = setInterval(() => {
      if (ws.readyState === WebSocket.CLOSING || ws.readyState === WebSocket.CLOSED)
        return clearInterval(pingInterval);

      let currentDate: Date = new Date();

      if (lastReceiveTime.getTime() - currentDate.getTime() > MaxTimeout) {
        debug('More than 30s do not receive data');
      }

      let pingTimeout: number = currentDate.getTime() - lastSendTime.value.getTime() - MaxTimeout;

      if (pingTimeout < 0) {
        return;
      }
      ws.send(formatMessage(terminalId, 'PING', ''));
    }, 25 * 1000);
  };

  const dispatch = (
    data: any,
    terminalId: Ref<string>,
    fitAddon: FitAddon,
    shareCode: any,
    currentUser: Ref<any>,
    setting: Ref<any>,
    emits: (event: 'wsData', msgType: string, msg: any, terminal: Terminal, setting: any) => void
  ) => {
    if (data === undefined) return;

    let msg = JSON.parse(data);
    switch (msg.type) {
      case 'CONNECT': {
        terminalId.value = msg.id;

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
        setting.value = info.setting;

        updateIcon(setting.value);

        ws.send(formatMessage(terminalId.value, 'TERMINAL_INIT', JSON.stringify(terminalData)));
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

    emits('wsData', msg.type, msg, terminal, setting.value);
  };

  const handleOnWebsocketMessage = (
    e: MessageEvent,
    terminal: Terminal,
    enableZmodem: boolean,
    zmodemStatus: boolean,
    lastSendTime: Ref<Date>,
    zsentry: ZmodemBrowser.Sentry,
    terminalId: Ref<string>,
    fitAddon: FitAddon,
    shareCode: any,
    currentUser: Ref<any>,
    setting: Ref<any>,
    emits: (event: 'wsData', msgType: string, msg: any, terminal: Terminal, setting: any) => void
  ) => {
    lastSendTime.value = new Date();

    if (typeof e.data === 'object') {
      if (enableZmodem) {
        zsentry.consume(e.data);
      } else {
        writeBufferToTerminal(enableZmodem, zmodemStatus, terminal, e.data);
      }
    } else {
      debug(typeof e.data);
      dispatch(e.data, terminalId, fitAddon, shareCode, currentUser, setting, emits);
    }
  };

  /**
   * @description 创建 WebSocket
   */
  const createWebSocket = (wsURL: string, term: Terminal, lastSend: Ref<Date>) => {
    ws = new WebSocket(wsURL, ['JMS-KOKO']);

    terminal = term;
    lastSendTime = lastSend;

    ws.binaryType = 'arraybuffer';
    ws.onerror = (e: Event) => {
      terminal.write('Connection Websocket Error');
      fireEvent(new Event('CLOSE', {}));
      handleError(e);
    };
    ws.onclose = (e: Event) => {
      terminal.write('Connection WebSocket Closed');
      fireEvent(new Event('CLOSE', {}));
      handleError(e);
    };

    return ws;
  };

  return {
    onWebsocketOpen,
    createWebSocket,
    handleOnWebsocketMessage
  };
};
