import { message } from 'antd';
import { useTranslation } from 'react-i18next';
import { preprocessInput, updateIcon } from '@/utils';
import { MESSAGE_TYPE, TERMINAL_MESSAGE_TYPE } from '@/enums';
import { useState, useCallback, useEffect, useRef, use } from 'react';

import useDetail from '@/store/useDetail';

import type { Terminal } from '@xterm/xterm';
import type { DetailMessage, OnlineUser, ShareUserOptions } from '@/types/detail.type';

interface Connection {
  terminal: Terminal;

  wsUrl: string;
}

export const useConnection = () => {
  const [terminalId, setTerminalId] = useState<string>('');
  const [socket, setSocket] = useState<WebSocket | null>(null);

  const enableShareRef = useRef(false);
  const sessionIdRef = useRef<string>('');
  const socketRef = useRef<WebSocket | null>(null);
  const terminalRef = useRef<Terminal | null>(null);

  const { t } = useTranslation();
  const { setConnectionInfo, setTerminalConfig, setShareInfo, terminalConfig, share } = useDetail();

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

        const info: DetailMessage = JSON.parse(parsedMessageData.data);

        const terminalSendData = {
          cols: terminal.cols,
          rows: terminal.rows,
          code: ''
        };

        setConnectionInfo({
          assetName: info.asset.name,
          address: info.asset.address,
          username: info.user.username
        });

        updateIcon(info.setting.INTERFACE!.favicon);

        enableShareRef.current = info.setting.SECURITY_SESSION_SHARE || false;

        currentSocket.send(
          JSON.stringify({
            id: newTerminalId,
            type: TERMINAL_MESSAGE_TYPE.TERMINAL_INIT,
            data: JSON.stringify(terminalSendData)
          })
        );

        break;
      }
      case MESSAGE_TYPE.TERMINAL_SESSION: {
        const sessionInfo = JSON.parse(parsedMessageData.data);
        const sessionDetail = sessionInfo.session;

        const share = sessionInfo?.permission?.actions?.includes('share');

        if (sessionInfo.backspaceAsCtrlH) {
          setTerminalConfig({
            backspaceAsCtrlH: sessionInfo.backspaceAsCtrlH
          });
        }

        if (sessionInfo.themeName) {
          setTerminalConfig({
            theme: sessionInfo.themeName
          });
        }

        if (enableShareRef.current && share) {
          setShareInfo({
            enabledShare: true
          });
        }

        setConnectionInfo({
          sessionId: sessionDetail.id
        });

        break;
      }
      case MESSAGE_TYPE.TERMINAL_SHARE_JOIN: {
        const data = JSON.parse(parsedMessageData.data);

        // data 中的 primary 字段如果不是 true 则表示的是加入的用户
        if (!data.primary) {
          message.info(`${data.user} ${t('JoinShare')}`);

          if (share.onlineUsers) {
            setShareInfo({
              onlineUsers: [...share.onlineUsers, data]
            });
          }
        }

        break;
      }
      case MESSAGE_TYPE.TERMINAL_PERM_VALID: {
        break;
      }
      case MESSAGE_TYPE.TERMINAL_SHARE_LEAVE: {
        const data: OnlineUser = JSON.parse(parsedMessageData.data);

        const userIndex = share.onlineUsers?.findIndex(item => item.user_id === data.user_id && !item.primary);

        if (userIndex !== -1) {
          setShareInfo({
            onlineUsers: share.onlineUsers?.filter(item => item.user_id !== data.user_id)
          });

          message.info(`${data.user} ${t('LeaveShare')}`);
        }

        break;
      }
      case MESSAGE_TYPE.TERMINAL_PERM_EXPIRED: {
        break;
      }
      case MESSAGE_TYPE.TERMINAL_SESSION_PAUSE: {
        const data = JSON.parse(parsedMessageData.data);
        message.info(`${data.user} ${t('PauseSession')}`);
        break;
      }
      case MESSAGE_TYPE.TERMINAL_GET_SHARE_USER: {
        const data: ShareUserOptions = JSON.parse(parsedMessageData.data);

        if (share.searchEnabledShareUser) {
          setShareInfo({
            searchEnabledShareUser: [...share.searchEnabledShareUser, data]
          });
        }

        break;
      }
      case MESSAGE_TYPE.TERMINAL_SESSION_RESUME: {
        const data = JSON.parse(parsedMessageData.data);
        message.info(`${data.user} ${t('ResumeSession')}`);
        break;
      }
      case MESSAGE_TYPE.TERMINAL_SHARE_USER_REMOVE: {
        message.info(t('RemoveShareUser'));
        socketRef.current?.close();
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
      terminal.write('\r\n');
      terminal.write('\r\n');
      message.info('Connection websocket has been closed');
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
      const processedData = preprocessInput(data, terminalConfig.backspaceAsCtrlH || '0');

      if (socketRef.current) {
        socketRef.current.send(
          JSON.stringify({
            id: terminalId,
            type: TERMINAL_MESSAGE_TYPE.TERMINAL_DATA,
            data: processedData
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
