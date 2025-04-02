import { ref } from 'vue';
import { Terminal } from '@xterm/xterm';

import { MaxTimeout } from '@/config';
import { writeBufferToTerminal } from '@/utils';
import {
  sendEventToLuna,
  updateIcon,
} from '@/components/TerminalComponent/helper';

enum FormatterMessageType {
  PING = 'PING',
  TERMINAL_INIT = 'TERMINAL_INIT',
  TERMINAL_DATA = 'TERMINAL_DATA',
  TERMINAL_RESIZE = 'TERMINAL_RESIZE',
  TERMINAL_K8S_DATA = 'TERMINAL_K8S_DATA',
  TERMINAL_K8S_RESIZE = 'TERMINAL_K8S_RESIZE',
}

enum SendLunaMessageType {
  PING = 'PING',
  CLOSE = 'CLOSE',
  PONG = 'PONG',
  CONNECT = 'CONNECT',
  TERMINAL_ERROR = 'TERMINAL_ERROR',
  MESSAGE_NOTIFY = 'MESSAGE_NOTIFY',
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

export const useTerminalConnection = (lunaId: string, origin: string) => {
  let terminalId = ref<string>('');
  let pingInterval = ref<number | null>(null);

  const lastSendTime = ref(new Date());
  const lastReceiveTime = ref(new Date());

  const formatMessage = (id: string, type: FormatterMessageType, data: any) => {
    return JSON.stringify({
      id,
      type,
      data
    });
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

      socket.send(formatMessage('', FormatterMessageType.PING, ''));
    });
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
        }

        const info = JSON.parse(parsedMessageData.data);

        // 设置 setting 和 当前用户

        // 更新网页图标
        updateIcon(info.setting);

        socket.send(formatMessage(terminalId.value, FormatterMessageType.TERMINAL_INIT, JSON.stringify(terminalData)));
        break;
      }
      case MessageType.TERMINAL_ACTION: {
        // todo
        break;
      }
      case MessageType.CLOSE: {
        socket.close();

        sendEventToLuna(SendLunaMessageType.CLOSE, '', lunaId, origin)
        break;
      }
      case MessageType.ERROR: {
        terminal.write(parsedMessageData.err);

        sendEventToLuna(SendLunaMessageType.TERMINAL_ERROR, '', lunaId, origin)
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
  }

  /**
   * @description 初始化 socket 事件
   * @param terminal
   * @param socket
   */
  const initializeSocketEvent = (terminal: Terminal, socket: WebSocket) => {
    socket.onopen = () => {
      socket.binaryType = 'arraybuffer';
      // heartBeat(socket);
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
        writeBufferToTerminal(true, false, terminal, event.data);
      } else {
        dispatch(event.data, terminal, socket);
      }
    }
  };

  return {
    initializeSocketEvent
  };
};
