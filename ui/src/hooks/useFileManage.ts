import { useRoute } from 'vue-router';
import { useMessage } from 'naive-ui';
import { useWebSocket } from '@vueuse/core';

import { BASE_WS_URL } from '@/config';

import type { RouteRecordNameGeneric } from 'vue-router';
import type { MessageApiInjection } from 'naive-ui/es/message/src/MessageProvider';
import { IFileManage, IFileManageConnectData, IFileManageSftpFileItem } from '@/hooks/interface';
import { useFileManageStore } from '@/store/modules/fileManage.ts';

enum MessageType {
  CONNECT = 'CONNECT',
  CLOSE = 'CLOSE',
  ERROR = 'ERROR',
  PING = 'PING',
  PONG = 'PONG',
  SFTP_DATA = 'SFTP_DATA'
}

/**
 * @description 获取文件管理的 url
 */
const getFileManageUrl = () => {
  const route = useRoute();

  const routeName: RouteRecordNameGeneric = route.name;
  const urlParams: URLSearchParams = new URLSearchParams(window.location.search.slice(1));

  let fileConnectionUrl: string = '';

  if (routeName === 'Terminal') {
    fileConnectionUrl = urlParams ? `${BASE_WS_URL}/koko/ws/sftp/?${urlParams.toString()}` : '';

    return fileConnectionUrl;
  }
};

/**
 * @description 处理 type 为 connect 的方法
 * @param messageData
 * @param id
 * @param socket
 */
const handleSocketConnectEvent = (messageData: IFileManageConnectData, id: string, socket: WebSocket) => {
  const sendData = '';

  const sendBody = {
    id,
    type: 'SFTP_DATA',
    cmd: 'list',
    data: sendData
  };

  if (messageData) {
    socket.send(JSON.stringify(sendBody));
  }
};

const handleSocketSftpData = (messageData: IFileManageSftpFileItem[]) => {
  const fileManageStore = useFileManageStore();

  fileManageStore.setFileList(messageData);
};

/**
 * @description 处理 message
 * @param socket
 */
const initSocketEvent = (socket: WebSocket) => {
  socket.binaryType = 'arraybuffer';

  socket.onopen = () => {};
  socket.onerror = () => {};

  socket.onclose = (event: CloseEvent) => {};
  socket.onmessage = (event: MessageEvent) => {
    const message: IFileManage = JSON.parse(event.data);

    switch (message.type) {
      case MessageType.CONNECT: {
        handleSocketConnectEvent(JSON.parse(message.data), message.id, socket);
        break;
      }

      case MessageType.SFTP_DATA: {
        handleSocketSftpData(JSON.parse(message.data));
        break;
      }

      default: {
        break;
      }
    }
  };
};

/**
 * @description 文件管理中的 Socket 连接
 * @param url
 * @param message
 */
const fileSocketConnection = (url: string, message: MessageApiInjection) => {
  const { data, status, close, open, send, ws } = useWebSocket(url, {
    protocols: ['JMS-KOKO'],
    autoReconnect: {
      retries: 5,
      delay: 3000
    }
  });

  if (!ws.value) {
    message.error('获取文件列表信息失败');
  }

  initSocketEvent(<WebSocket>ws!.value);

  return;
};

/**
 * @description 用于处理文件管理相关逻辑
 */
export const useFileManage = () => {
  let fileConnectionUrl: string | undefined = '';

  const message = useMessage();

  function init() {
    fileConnectionUrl = getFileManageUrl();

    if (fileConnectionUrl) {
      fileSocketConnection(fileConnectionUrl, message);
    }
  }

  init();
};
