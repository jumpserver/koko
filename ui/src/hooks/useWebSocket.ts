import { Ref } from 'vue';
import { Terminal } from '@xterm/xterm';
import { fireEvent } from '@/utils';
import { useLogger } from '@/hooks/useLogger.ts';
import { MaxTimeout } from '@/config';
import { formatMessage, handleError, sendEventToLuna } from '@/components/Terminal/helper';

const { debug } = useLogger('WebSocketManager');

export const useWebSocket = () => {
  let ws: WebSocket;
  let terminal: Terminal;
  let lastReceiveTime: Date;

  let terminalId: string;
  let pingInterval: number;

  let lastSendTime: Ref<Date>;

  /**
   * @description 在 WebSocket 连接成功建立时触发的回调
   */
  const onWebsocketOpen = () => {
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

  /**
   * @description 创建 WebSocket
   */
  const createWebSocket = (wsURL: string, term: Terminal, lastSend: Ref<Date>) => {
    ws = new WebSocket(wsURL, ['JMS-KOKO']);

    terminal = term;
    lastSendTime = lastSend;

    ws.binaryType = 'arraybuffer';
    ws.onopen = onWebsocketOpen;
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
    createWebSocket
  };
};
