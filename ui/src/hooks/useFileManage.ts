import { useRoute } from 'vue-router';
import { type UploadFileInfo, useMessage } from 'naive-ui';
import { useWebSocket } from '@vueuse/core';
import { useFileManageStore } from '@/store/modules/fileManage.ts';

import { v4 as uuid } from 'uuid';
import { BASE_WS_URL } from '@/config';

import mittBus from '@/utils/mittBus.ts';

import type { Ref } from 'vue';
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
    fileConnectionUrl = urlParams ? `${BASE_WS_URL}/koko/ws/sftp/?token=${urlParams.toString().split('&')[1].split('=')[1]}` : '';

    return fileConnectionUrl;
  }
};

/**
 * @description 将 buffer 转为 base64
 * @param buffer
 */
const arrayBufferToBase64 = (buffer: ArrayBuffer): string => {
  const uint8Array = new Uint8Array(buffer);
  const CHUNK_SIZE = 0x8000;

  let result = '';

  for (let i = 0; i < uint8Array.length; i += CHUNK_SIZE) {
    const chunk = uint8Array.subarray(i, i + CHUNK_SIZE);
    result += String.fromCharCode.apply(null, chunk as unknown as number[]);
  }

  return btoa(result);
};

/**
 * @description 刷新文件列表
 * @param socket
 * @param path
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

  if (fileManageStore.currentPath === '/') {
    messageData = [...messageData];
  } else {
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
  }

  fileManageStore.setFileList(messageData);
};

/**
 * @description 心跳检测机制
 * @param socket WebSocket实例
 */
const heartBeat = (socket: WebSocket) => {
  let pingInterval: number | null = null;
  
  const sendPing = () => {
    if (socket.CLOSED === socket.readyState || socket.CLOSING === socket.readyState) {
      clearInterval(pingInterval!);
      return;
    }

    const pingMessage = {
      id: uuid(),
      type: MessageType.PING,
      data: 'ping'
    };

    socket.send(JSON.stringify(pingMessage));
  };

  sendPing();

  pingInterval = window.setInterval(sendPing, 2000);

  return () => {
    if (pingInterval) {
      clearInterval(pingInterval);
    }
  };
};

/**
 * @description 处理 message
 * @param socket
 */
const initSocketEvent = (socket: WebSocket) => {
  const globalMessage = useMessage();
  const fileManageStore = useFileManageStore();

  let receivedBuffers: any = [];
  let clearHeartbeat: (() => void) | null = null;

  socket.binaryType = 'arraybuffer';

  socket.onopen = () => { clearHeartbeat = heartBeat(socket) };
  socket.onerror = () => { clearHeartbeat?.() };
  socket.onclose = () => { clearHeartbeat?.() };
  
  socket.onmessage = (event: MessageEvent) => {
    const message: IFileManage = JSON.parse(event.data);

    fileManageStore.setMessageId(message.id);
    fileManageStore.setCurrentPath(message.current_path);

    switch (message.type) {
      case MessageType.CONNECT: {
        handleSocketConnectEvent(JSON.parse(message.data), message.id, socket);
        break;
      }

      case MessageType.SFTP_DATA: {
        if (message.cmd === 'mkdir' && message.data === 'ok') {
          globalMessage.success('创建成功');

          mittBus.emit('reload-table');
        }

        if (message.cmd === 'rm' && message.data === 'ok') {
          globalMessage.success('删除成功');

          mittBus.emit('reload-table');
        }

        if (message.cmd === 'rename' && message.data === 'ok') {
          globalMessage.success('修改成功');

          mittBus.emit('reload-table');
        }

        if (message.cmd === 'upload' && message.data) {
          fileManageStore.setReceived(true);

          socket.send(
            JSON.stringify({
              cmd: 'upload',
              type: 'SFTP_DATA',
              id: '',
              raw: '',
              data: JSON.stringify({
                offSet: '',
                merge: true,
                size: '',
                path: message.data
              })
            })
          );
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
        globalMessage.error('Error Occurred!');

        break;
      }

      case MessageType.PING: {
        socket.send(JSON.stringify({
          id: uuid(),
          type: MessageType.PONG,
          data: 'pong'
        }));
        break;
      }
    
      case MessageType.PONG: {
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
  const { ws } = useWebSocket(url, {
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
const handleFileDownload = (socket: WebSocket, path: string, is_dir: boolean) => {
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
 * @description 预处理 chunks
 * @param sliceChunk
 * @param socket
 * @param CHUNK_SIZE
 * @param fileInfo
 * @param sentChunks
 */
const generateUploadChunks = async (
  sliceChunk: Blob,
  socket: WebSocket,
  fileInfo: UploadFileInfo,
  CHUNK_SIZE: number,
  sentChunks: number
) => {
  const fileManageStore = useFileManageStore();

  const sendData = {
    offSet: 0,
    merge: false,
    chunk: true,
    size: fileInfo.file?.size,
    path: `${fileManageStore.currentPath}/${fileInfo.name}`
  };

  const sendBody = {
    cmd: 'upload',
    type: 'SFTP_DATA',
    id: Math.floor(Math.random() * Number.MAX_SAFE_INTEGER).toString(),
    data: '',
    raw: ''
  };

  const arrayBuffer: ArrayBuffer = await sliceChunk.arrayBuffer();
  const base64String: string = arrayBufferToBase64(arrayBuffer);

  sendData.offSet = sentChunks * CHUNK_SIZE;
  sendBody.raw = base64String;
  sendBody.data = JSON.stringify(sendData);

  socket.send(JSON.stringify(sendBody));

  sentChunks++;

  return new Promise<boolean>(resolve => {
    const interval = setInterval(() => {
      if (fileManageStore.isReceived) {
        clearInterval(interval);
        resolve(true);
      }
    }, 100);
  });
};

/**
 * @description 上传文件
 */
const handleFileUpload = (
  socket: WebSocket,
  fileList: Ref<Array<UploadFileInfo>>,
  onProgress: any,
  onFinish: () => void,
  onError: () => void
) => {
  const fileManageStore = useFileManageStore();
  let CHUNK_SIZE = 1024 * 1024 * 5;

  fileList.value.forEach(async (fileInfo: UploadFileInfo) => {
    if (fileInfo.file) {
      let sliceCount = Math.ceil(fileInfo.file?.size / CHUNK_SIZE);
      let sliceChunks = [];

      for (let i = 0; i < sliceCount; i++) {
        sliceChunks.push(fileInfo.file.slice(i * CHUNK_SIZE, (i + 1) * CHUNK_SIZE));
      }

      let sentChunks = 0;

      try {
        for (const sliceChunk of sliceChunks) {
          fileManageStore.setReceived(false);
          await generateUploadChunks(sliceChunk, socket, fileInfo, CHUNK_SIZE, sentChunks);

          sentChunks++;

          const percent = (sentChunks / sliceChunks.length) * 100;

          console.log(
            '%c DEBUG[ percent ]-79:',
            'font-size:13px; background:#F0FFF0; color:#800000;',
            percent
          );

          onProgress({ percent });

          if (percent === 100) {
            onFinish();

            mittBus.emit('reload-table');
          }
        }
      } catch (e) {
        onError();
      }
    }
  });
};

/**
 * @description 用于处理文件管理相关逻辑
 */
export const useFileManage = (fileConnectionUrl: string) => {
  const message = useMessage();
  let socket: WebSocket | null = null;

  if (!fileConnectionUrl) {
    throw new Error('无法获取文件管理连接地址');
  }

  function cleanup() {
    if (socket) {
      socket.close();
      mittBus.off('file-upload');
      mittBus.off('download-file');
      mittBus.off('file-manage');
    }
  }

  function init() {
    try {
      const ws = fileSocketConnection(fileConnectionUrl, message);

      if (!ws) {
        throw new Error('WebSocket 连接失败');
      }

      socket = ws;

      mittBus.on(
        'file-upload',
        ({
          fileList,
          onFinish,
          onError,
          onProgress
        }: {
          fileList: Ref<Array<UploadFileInfo>>;
          onFinish: () => void;
          onError: () => void;
          onProgress: (e: { percent: number }) => void;
        }) => {
          if (socket) {
            handleFileUpload(socket, fileList, onProgress, onFinish, onError);
          }
        }
      );

      mittBus.on('download-file', ({ path, is_dir }: { path: string; is_dir: boolean }) => {
        if (socket) {
          handleFileDownload(socket, path, is_dir);
        }
      });

      mittBus.on(
        'file-manage',
        ({ path, type, new_name }: { path: string; type: ManageTypes; new_name?: string }) => {
          if (!socket) return;

          switch (type) {
            case ManageTypes.CREATE: {
              handleFileCreate(socket, path);
              break;
            }
            case ManageTypes.CHANGE: {
              handleChangePath(socket, path);
              break;
            }
            case ManageTypes.REFRESH: {
              refresh(socket, path);
              break;
            }
            case ManageTypes.RENAME: {
              handleFileRename(socket, path, new_name!);
              break;
            }
            case ManageTypes.REMOVE: {
              handleFileRemove(socket, path);
              break;
            }
          }
        }
      );

      return {
        cleanup,
        socket
      };
    } catch (error) {
      cleanup();
      console.error('File management initialization failed:', error);
      throw error;
    }
  }

  return init();
};

export const unloadListeners = () => {
  mittBus.off('download-file');
};
