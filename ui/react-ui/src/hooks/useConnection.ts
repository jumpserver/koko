import { message } from 'antd';
import { emitterEvent } from '@/utils';
import { useState, useCallback, useEffect, useRef } from 'react';
import { MESSAGE_TYPE, TERMINAL_MESSAGE_TYPE } from '@/enums';

import type { Terminal } from '@xterm/xterm';

interface Connection {
  terminal: Terminal;

  wsUrl: string;
}

export const useConnection = () => {
  const [terminalId, setTerminalId] = useState<string>('');
  const [socket, setSocket] = useState<WebSocket | null>(null);

  const socketRef = useRef<WebSocket | null>(null);
  const terminalRef = useRef<Terminal | null>(null);

  /**
   * @description 创建 socket
   * @param url
   * @returns
   */
  const createSocket = (url: string) => {
    const ws = new WebSocket(url, ['JMS-KOKO']);

    ws.binaryType = 'arraybuffer';
    setSocket(ws);
    // 立即设置 ref，不等待状态更新
    socketRef.current = ws;

    return ws;
  };

  /**
   * @description 处理消息
   */
  const dispatchEvent = (data: string, terminal: Terminal) => {
    const currentSocket = socketRef.current;

    if (!data || !currentSocket) return;

    const parsedMessageData = JSON.parse(data);

    switch (parsedMessageData.type) {
      case MESSAGE_TYPE.CLOSE: {
        break;
      }
      case MESSAGE_TYPE.ERROR: {
        message.error(parsedMessageData.err);
        break;
      }
      case MESSAGE_TYPE.PING: {
        break;
      }
      case MESSAGE_TYPE.CONNECT: {
        const newTerminalId = parsedMessageData.id;

        setTerminalId(newTerminalId);

        const info = JSON.parse(parsedMessageData.data);

        const terminalSendData = {
          cols: terminal.cols,
          rows: terminal.rows,
          code: ''
        };

        currentSocket.send(
          JSON.stringify({
            id: newTerminalId,
            type: TERMINAL_MESSAGE_TYPE.TERMINAL_INIT,
            data: JSON.stringify(terminalSendData)
          })
        );

        break;
      }
    }
  };

  /**
   * @description 初始化连接
   * @param terminal
   * @param wsUrl
   */
  const initializeConnection = useCallback(({ terminal, wsUrl }: Connection) => {
    const ws = createSocket(wsUrl);

    ws.onopen = () => {
      // TODO 设置心跳
      // message.success('正在开始连接到目标主机');
    };

    ws.onmessage = (event: MessageEvent) => {
      // 处理二进制数据
      if (typeof event.data === 'object') {
        terminal.write(new Uint8Array(event.data));
      } else {
        dispatchEvent(event.data, terminal);
      }
    };

    ws.onclose = () => {
      message.error('连接已关闭');

      terminal.write('\r\n');
      terminal.write('\r\n');
      terminal.write('\r\n\r\n\x1b[31mConnection websocket has been closed\x1b[0m');
    };

    ws.onerror = error => {
      console.log('WebSocket 连接错误', error);
    };

    terminalRef.current = terminal;

    terminal.onResize(({ cols, rows }) => {
      console.log('终端大小', cols, rows);

      ws.send(
        JSON.stringify({
          id: terminalId,
          type: TERMINAL_MESSAGE_TYPE.TERMINAL_RESIZE,
          data: JSON.stringify({ cols, rows })
        })
      );
    });

    terminal.onData(data => {
      console.log('终端数据', data);

      if (socketRef.current) {
        socketRef.current.send(
          JSON.stringify({
            id: terminalId,
            type: TERMINAL_MESSAGE_TYPE.TERMINAL_DATA,
            data
          })
        );
      }
    });
  }, []);

  useEffect(() => {
    socketRef.current = socket;
  }, [socket]);

  return {
    socketRef,
    terminalId,
    initializeConnection
  };
};
