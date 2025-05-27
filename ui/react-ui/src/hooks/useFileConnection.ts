import { useRef } from 'react';
import { message } from 'antd';
import { v4 as uuid } from 'uuid';
import { getConnectionUrl } from '@/utils';
import { useFileStatus } from '@/store/useFileStatus';
import { FILE_MANAGE_MESSAGE_TYPE, SFTP_CMD } from '@/enums';

import type { FileMessage, FileItem } from '@/types/file.type';

export const useFileConnection = () => {
  const socket = useRef<WebSocket | null>(null);
  const messageId = useRef<string>('');
  const currentPath = useRef<string>('');

  const { setFileMessage } = useFileStatus();

  /**
   * @description 处理连接事件
   */
  const handleTypeConnectEvent = () => {
    const sendData = {
      path: ''
    };

    const sendBody = {
      id: messageId.current,
      type: FILE_MANAGE_MESSAGE_TYPE.SFTP_DATA,
      cmd: SFTP_CMD.LIST,
      data: JSON.stringify(sendData)
    };

    if (socket.current) {
      socket.current.send(JSON.stringify(sendBody));
    }
  };

  /**
   * @description 处理 SFTP 数据事件
   * @param fileMessage
   */
  const handleTypeSftpDataEvent = (fileMessage: FileMessage) => {
    const { cmd, data, current_path } = fileMessage;

    switch (cmd) {
      case SFTP_CMD.LIST:
        {
          currentPath.current = current_path;

          setFileMessage({
            paths: current_path.split('/')[1],
            fileList: JSON.parse(data)
          });
        }

        break;
      case SFTP_CMD.MKDIR:
        break;
      case SFTP_CMD.MKFILE:
        break;
      case SFTP_CMD.RENAME:
    }
  };

  /**
   * @description 处理文件连接事件
   * @param message
   */
  const handleFileConnectEvent = (message: MessageEvent) => {
    const fileMessage: FileMessage = JSON.parse(message.data);

    messageId.current = fileMessage.id;
    currentPath.current = fileMessage.current_path;

    switch (fileMessage.type) {
      case FILE_MANAGE_MESSAGE_TYPE.PING:
        socket.current?.send(
          JSON.stringify({
            id: uuid(),
            type: FILE_MANAGE_MESSAGE_TYPE.PONG,
            data: 'pong'
          })
        );
        break;
      case FILE_MANAGE_MESSAGE_TYPE.ERROR:
        break;
      case FILE_MANAGE_MESSAGE_TYPE.CONNECT:
        handleTypeConnectEvent();
        break;
      case FILE_MANAGE_MESSAGE_TYPE.SFTP_DATA:
        handleTypeSftpDataEvent(fileMessage);
        break;
      case FILE_MANAGE_MESSAGE_TYPE.SFTP_BINARY:
        // handleTypeSftpBinaryEvent(fileMessage);
        break;
    }
  };

  /**
   * @description 处理刷新事件
   */
  const handleRefresh = () => {
    const sendBody = {
      id: uuid(),
      cmd: SFTP_CMD.LIST,
      type: FILE_MANAGE_MESSAGE_TYPE.SFTP_DATA,
      data: JSON.stringify({
        path: currentPath.current
      })
    };

    socket.current?.send(JSON.stringify(sendBody));
  };

  const createFileSocket = (token: string) => {
    const wsUrl = getConnectionUrl('ws');

    const ws = new WebSocket(`${wsUrl}/koko/ws/sftp/?token=${token}`, ['JMS-KOKO']);
    ws.binaryType = 'arraybuffer';

    socket.current = ws;

    ws.onopen = () => {
      // TODO 心跳
      message.success('SFTP Connection Success');
    };

    ws.onerror = () => {};

    ws.onclose = () => {
      message.error('SFTP connection has been closed');
    };

    ws.onmessage = (message: MessageEvent) => {
      handleFileConnectEvent(message);
    };
  };

  return {
    handleRefresh,
    createFileSocket
  };
};
