import { message } from 'antd';
import { useState, useCallback, useEffect, useRef } from 'react';
import { formatMessage } from '@/utils';
import { MESSAGE_TYPE, TERMINAL_MESSAGE_TYPE } from '@/enums';
import type { Terminal } from '@xterm/xterm';

interface Connection {
  terminal: Terminal;

  wsUrl: string;
}

export const useConnection = () => {
  const [terminalId, setTerminalId] = useState<string>('');
  const [socket, setSocket] = useState<WebSocket | null>(null);

  // 使用 ref 跟踪最新值，解决闭包问题
  const socketRef = useRef<WebSocket | null>(null);

  // 同步 socket 状态到 ref
  useEffect(() => {
    socketRef.current = socket;
  }, [socket]);

  const createSocket = useCallback((url: string) => {
    const ws = new WebSocket(url, ['JMS-KOKO']);

    ws.binaryType = 'arraybuffer';
    setSocket(ws);
    // 立即设置 ref，不等待状态更新
    socketRef.current = ws;

    return ws;
  }, []);

  // prettier-ignore
  const dispatchEvent = useCallback((data: string, terminal: Terminal) => {
      // 使用 ref 而不是状态来获取最新的 socket
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

          currentSocket.send(JSON.stringify({
            id: newTerminalId,
            type: TERMINAL_MESSAGE_TYPE.TERMINAL_INIT,
            data: JSON.stringify(terminalSendData)
          }));

          break;
        }
      }
    },
    []
  );

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

    terminal.onData(data => {
      console.log('终端数据', data);

      if (socketRef.current) {
        // 使用与 CONNECT 消息相同的格式发送数据
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

  return {
    socketRef,
    terminalId,
    initializeConnection
  };
};
