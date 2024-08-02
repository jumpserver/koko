import { Ref } from 'vue';
import { Terminal } from '@xterm/xterm';
import { fireEvent, writeBufferToTerminal } from '@/utils';
import { useLogger } from '@/hooks/useLogger.ts';
import { BASE_WS_URL, MaxTimeout } from '@/config';
import {
  formatMessage,
  handleError,
  sendEventToLuna,
  updateIcon
} from '@/components/Terminal/helper';
import { createDiscreteApi } from 'naive-ui';
import ZmodemBrowser from 'nora-zmodemjs/src/zmodem_browser';
import { FitAddon } from '@xterm/addon-fit';

import { io, Socket } from 'socket.io-client';
import { useRoute } from 'vue-router';

const { message } = createDiscreteApi(['message']);

const { debug } = useLogger('useWebSocket');

export const useWebSocket = (
  enableZmodem: boolean,
  zmodemStatus: Ref<boolean>,
  zsentryRef: Ref<ZmodemBrowser.Sentry | null>,
  fitAddon: FitAddon,
  shareCode: any,
  currentUser: Ref<any>,
  setting: Ref<any>,
  emits: (event: 'wsData', msgType: string, msg: any, terminal: Terminal, setting: any) => void
): any => {
  let ws: Socket;
  let terminal: Terminal;
  let lastReceiveTime: Date;
  let id: Ref<string>;

  let pingInterval: number;

  let lastSendTime: Ref<Date>;

  const handleMessage = (e: MessageEvent) => {
    lastSendTime.value = new Date();

    if (typeof e.data === 'object') {
      if (enableZmodem) {
        zsentryRef.value?.consume(e.data);
      } else {
        writeBufferToTerminal(enableZmodem, zmodemStatus.value, terminal, e.data);
      }
    } else {
      debug(typeof e.data);
      dispatch(e.data);
    }
  };

  const onWebsocketOpen = (terminalId: string, socket: Socket) => {
    console.log('-----------');

    sendEventToLuna('CONNECTED', '');

    if (pingInterval !== null) clearInterval(pingInterval);

    lastReceiveTime = new Date();

    pingInterval = setInterval(() => {
      if (socket.disconnected) return clearInterval(pingInterval);

      let currentDate: Date = new Date();

      if (lastReceiveTime.getTime() - currentDate.getTime() > MaxTimeout) {
        debug('More than 30s do not receive data');
      }

      let pingTimeout: number = currentDate.getTime() - lastSendTime.value.getTime() - MaxTimeout;

      if (pingTimeout < 0) return;

      socket.emit('PING', terminalId, '');
      // ws.send(formatMessage(terminalId, 'PING', ''));
    }, 25 * 1000);
  };

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
        setting.value = info.setting;

        updateIcon(setting.value);

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

    emits('wsData', msg.type, msg, terminal, setting.value);
  };

  const generateWsURL = () => {
    const route = useRoute();

    const routeName = route.name;
    const urlParams = new URLSearchParams(window.location.search.slice(1));

    let path, query;

    switch (routeName) {
      case 'Token': {
        const params = route.params;
        const requireParams = new URLSearchParams();

        requireParams.append('type', 'token');
        requireParams.append('target_id', params.id as string);

        path = '/koko/ws/token/';
        query = requireParams.toString();
        break;
      }
      case 'TokenParams': {
        path = '/koko/ws/token/';
        query = urlParams.toString();
        break;
      }
      default: {
        path = '/koko/ws/terminal/';
        query = urlParams.toString();
      }
    }

    return {
      path,
      query
    };
  };

  const createWebSocket = (term: Terminal, lastSend: Ref<Date>, terminalId: Ref<string>) => {
    if (ws) {
      // 清理旧的监听器和定时器
      ws.removeAllListeners();
      if (pingInterval !== null) clearInterval(pingInterval);
      ws.close();
    }

    id = terminalId;
    terminal = term;
    lastSendTime = lastSend;

    const { path, query } = generateWsURL();

    const socket = io(BASE_WS_URL, {
      path,
      transports: ['websocket'],
      query: Object.fromEntries(new URLSearchParams(query).entries()),
      protocols: ['JMS-KOKO']
    });

    console.log(socket);
    console.log(socket.connected);

    socket.on('connect', () => {
      console.log('connect');
      onWebsocketOpen(terminalId.value, socket);
    });

    // 暴露出 onmessage
    socket.on('message', (e: MessageEvent) => {
      console.log(112312312312);
      handleMessage(e);
    });

    socket.on('error', error => {
      terminal.write('Connection Websocket Error');
      fireEvent(new Event('CLOSE', {}));
      handleError(error);
    });

    socket.on('disconnect', (e: Socket.DisconnectReason) => {
      terminal.write('Connection WebSocket Closed');
      fireEvent(new Event('CLOSE', {}));
      handleError(e);
    });

    ws = socket;

    return socket;
  };

  return {
    createWebSocket,
    handleMessage
  };
};
