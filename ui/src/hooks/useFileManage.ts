import { useRoute } from 'vue-router';
import { useMessage } from 'naive-ui';
import { useWebSocket } from '@vueuse/core';
import { useFileManageStore } from '@/store/modules/fileManage.ts';

import { BASE_WS_URL } from '@/config';

import mittBus from '@/utils/mittBus.ts';
import { v4 as uuid } from 'uuid';

import type { RouteRecordNameGeneric } from 'vue-router';
import type { MessageApiInjection } from 'naive-ui/es/message/src/MessageProvider';
import type { IFileManage, IFileManageConnectData, IFileManageSftpFileItem } from '@/hooks/interface';

export enum MessageType {
  CONNECT = 'CONNECT',
  CLOSE = 'CLOSE',
  ERROR = 'ERROR',
  PING = 'PING',
  PONG = 'PONG',
  SFTP_DATA = 'SFTP_DATA',
  SFTP_BINARY = 'SFTP_BINARY'
}
export enum ManageTypes {
  CREATE = 'CREATE',
  CHANGE = 'CHANGE',
  REFRESH = 'REFRESH',
  RENAME = 'RENAME',
  REMOVE = 'REMOVE'
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
 * @description 刷新文件列表
 * @param socket
 */
export const refresh = (socket: WebSocket, path: string) => {
  const sendData = {
    path
  };

  const sendBody = {
    id: uuid(),
    cmd: 'list',
    type: 'SFTP_DATA',
    data: JSON.stringify(sendData)
  };

  socket.send(JSON.stringify(sendBody));
};

/**
 * @description 处理 type 为 connect 的方法
 * @param messageData
 * @param id
 * @param socket
 */
const handleSocketConnectEvent = (messageData: IFileManageConnectData, id: string, socket: WebSocket) => {
  const sendData = {
    path: ''
  };

  const sendBody = {
    id,
    type: 'SFTP_DATA',
    cmd: 'list',
    data: JSON.stringify(sendData)
  };

  if (messageData) {
    socket.send(JSON.stringify(sendBody));
  }
};

/**
 * @description 设置文件信息 table
 * @param messageData
 */
const handleSocketSftpData = (messageData: IFileManageSftpFileItem[]) => {
  const fileManageStore = useFileManageStore();

  messageData = [
    {
      name: '..',
      size: '',
      perm: '',
      mod_time: '',
      type: '',
      is_dir: true
    },
    ...messageData
  ];

  fileManageStore.setFileList(messageData);
};

/**
 * @description 处理 message
 * @param socket
 */
const initSocketEvent = (socket: WebSocket) => {
  const globalMessage = useMessage();
  const fileManageStore = useFileManageStore();

  let receivedBuffers: any = [];

  socket.binaryType = 'arraybuffer';

  socket.onopen = () => {};
  socket.onerror = () => {};

  socket.onclose = (event: CloseEvent) => {};
  socket.onmessage = (event: MessageEvent) => {
    const message: IFileManage = JSON.parse(event.data);

    fileManageStore.setMessageId(message.id);
    fileManageStore.setCurrentPath(message.current_path);

    console.log('=>(useFileManage.ts:84) message', message);

    switch (message.type) {
      case MessageType.CONNECT: {
        handleSocketConnectEvent(JSON.parse(message.data), message.id, socket);
        break;
      }

      case MessageType.SFTP_DATA: {
        if (message.cmd === 'mkdir' && message.data === 'ok') {
          globalMessage.success('创建成功');
        }

        if (message.cmd === 'rm' && message.data === 'ok') {
          globalMessage.success('删除成功');
        }

        if (message.cmd === 'download' && message.data) {
          const blob: Blob = new Blob(receivedBuffers, { type: 'application/octet-stream' });

          const url = window.URL.createObjectURL(blob);
          const a = document.createElement('a');

          a.style.display = 'none';
          a.href = url;
          a.download = message.data;

          document.body.appendChild(a);
          a.click();

          window.URL.revokeObjectURL(url);
          document.body.removeChild(a);
          receivedBuffers = [];
        }

        if (message.cmd === 'list') {
          handleSocketSftpData(JSON.parse(message.data));
        }

        break;
      }

      case MessageType.SFTP_BINARY: {
        receivedBuffers.push(message.raw);

        break;
      }

      case MessageType.ERROR: {
        globalMessage.error('Error Occured!');

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

  return ws.value;
};

/**
 * @description 路径跳转的处理
 * @param socket
 * @param path
 */
const handleChangePath = (socket: WebSocket, path: string) => {
  const sendBody = {
    id: uuid(),
    type: 'SFTP_DATA',
    cmd: 'list',
    data: JSON.stringify({ path })
  };

  socket.send(JSON.stringify(sendBody));
};

/**
 * @description 创建文件夹
 * @param socket
 * @param path
 */
const handleFileCreate = (socket: WebSocket, path: string) => {
  const sendBody = {
    id: uuid(),
    type: 'SFTP_DATA',
    cmd: 'mkdir',
    data: JSON.stringify({ path })
  };

  socket.send(JSON.stringify(sendBody));
};

/**
 * @description 重命名
 * @param socket
 * @param path
 * @param newName
 */
const handleFileRename = (socket: WebSocket, path: string, newName: string) => {
  const sendBody = {
    id: uuid(),
    type: 'SFTP_DATA',
    cmd: 'rename',
    data: JSON.stringify({ path, new_name: newName })
  };

  socket.send(JSON.stringify(sendBody));
};

/**
 * @description 移除文件
 * @param socket
 * @param path
 */
const handleFileRemove = (socket: WebSocket, path: string) => {
  const sendBody = {
    id: uuid(),
    type: 'SFTP_DATA',
    cmd: 'rm',
    data: JSON.stringify({ path })
  };

  socket.send(JSON.stringify(sendBody));
};

/**
 * @description 下载文件
 * @param socket
 * @param path
 * @param is_dir
 */
export const handleFileDownload = (socket: WebSocket, path: string, is_dir: boolean) => {
  const sendData = {
    path,
    is_dir
  };

  const sendBody = {
    id: uuid(),
    type: 'SFTP_DATA',
    cmd: 'download',
    data: JSON.stringify(sendData)
  };

  socket.send(JSON.stringify(sendBody));
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
      const socket = fileSocketConnection(fileConnectionUrl, message);

      mittBus.on('download-file', ({ path, is_dir }: { path: string; is_dir: boolean }) => {
        handleFileDownload(<WebSocket>socket, path, is_dir);
      });

      mittBus.on(
        'file-manage',
        ({ path, type, new_name }: { path: string; type: ManageTypes; new_name?: string }) => {
          switch (type) {
            case ManageTypes.CREATE: {
              handleFileCreate(<WebSocket>socket, path);
              break;
            }
            case ManageTypes.CHANGE: {
              handleChangePath(<WebSocket>socket, path);
              break;
            }
            case ManageTypes.REFRESH: {
              refresh(<WebSocket>socket, path);
              break;
            }
            case ManageTypes.RENAME: {
              handleFileRename(<WebSocket>socket, path, new_name!);
              break;
            }
            case ManageTypes.REMOVE: {
              handleFileRemove(<WebSocket>socket, path);
              break;
            }
          }
        }
      );
    }
  }

  init();
};

export const unloadListeners = () => {
  mittBus.off('download-file');
};
