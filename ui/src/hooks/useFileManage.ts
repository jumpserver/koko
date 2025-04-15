import { useRoute } from 'vue-router';
import { computed, ref, watch } from 'vue';
import { useWebSocket } from '@vueuse/core';
import { createDiscreteApi, darkTheme } from 'naive-ui';
import { useFileManageStore } from '@/store/modules/fileManage.ts';

import { v4 as uuid } from 'uuid';
import { BASE_WS_URL } from '@/config';

import mittBus from '@/utils/mittBus.ts';

import type { Ref } from 'vue';
import type { ConfigProviderProps, UploadFileInfo } from 'naive-ui';
import type { MessageApiInjection } from 'naive-ui/es/message/src/MessageProvider';
import type { IFileManage, IFileManageConnectData, IFileManageSftpFileItem } from '@/hooks/interface';

export enum MessageType {
  CONNECT = 'CONNECT',
  CLOSE = 'CLOSE',
  ERROR = 'ERROR',
  PING = 'PING',
  PONG = 'PONG',
  CLOSED = 'closed',
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

const configProviderPropsRef = computed<ConfigProviderProps>(() => ({
  theme: darkTheme
}));
const { message: globalTipsMessage }: { message: MessageApiInjection } = createDiscreteApi(['message'], {
  configProviderProps: configProviderPropsRef
});

// TODO 都是 hook 内部状态
let initialPath = '';
let fileSize = '';
let uploadFileId = ref('');
let interrupt = ref(false);
let isStop = ref(false);
let downLoadMessage = null;

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

  // 初始化时保存初始路径
  if (initialPath === '') {
    initialPath = fileManageStore.currentPath;
  }

  // 如果当前路径是根目录或者是初始路径，则不添加 .. 文件夹
  if (fileManageStore.currentPath === '/' || fileManageStore.currentPath === initialPath) {
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

  pingInterval = window.setInterval(sendPing, 200000);

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
const initSocketEvent = (socket: WebSocket, t: any) => {
  const fileManageStore = useFileManageStore();

  let receivedBuffers: any = [];
  let clearHeartbeat: (() => void) | null = null;

  socket.binaryType = 'arraybuffer';

  socket.onopen = () => {
    clearHeartbeat = heartBeat(socket);
  };
  socket.onerror = () => {
    clearHeartbeat?.();
  };
  socket.onclose = () => {
    clearHeartbeat?.();
  };

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
          globalTipsMessage.success(t('OperationSuccessful'));

          mittBus.emit('reload-table');
        }

        if (message.cmd === 'rm' && message.data === 'ok') {
          globalTipsMessage.success(t('OperationSuccessful'));

          mittBus.emit('reload-table');
        }

        if (message.cmd === 'rm' && message.err === 'permission denied') {
          globalTipsMessage.error(t('PermissionDenied'));
        }

        if (message.cmd === 'rename' && message.data === 'ok') {
          globalTipsMessage.success(t('OperationSuccessful'));

          mittBus.emit('reload-table');
        }

        if (message.cmd === 'upload' && message.data === 'ok') {
          fileManageStore.setReceived(true);
          globalTipsMessage.success(t('UploadSuccess'));
        }

        if (message.cmd === 'upload' && message.data !== 'ok') {
          fileManageStore.setReceived(true);
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
          downLoadMessage!.destroy();
        }

        if (message.cmd === 'list') {
          handleSocketSftpData(JSON.parse(message.data));
        }
        break;
      }

      case MessageType.SFTP_BINARY: {
        const binaryString = atob(message.raw);
        const len = binaryString.length;
        const bytes = new Uint8Array(len);

        for (let i = 0; i < len; i++) {
          bytes[i] = binaryString.charCodeAt(i);
        }

        receivedBuffers.push(bytes);

        let receivedBytes = 0;

        for (const buffer of receivedBuffers) {
          receivedBytes += buffer.length;
        }

        const percent = (receivedBytes / Number(fileSize)) * 100;
        downLoadMessage!.content = `Downloading: ${percent.toFixed(2)}%`;

        break;
      }

      case MessageType.ERROR: {
        fileManageStore.setFileList([]);

        break;
      }

      case MessageType.PING: {
        socket.send(
          JSON.stringify({
            id: uuid(),
            type: MessageType.PONG,
            data: 'pong'
          })
        );
        break;
      }

      case MessageType.PONG: {
        break;
      }

      case MessageType.CLOSED: {
        interrupt.value = true;
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
 */
const fileSocketConnection = (url: string, t: any) => {
  const { ws } = useWebSocket(url, {
    protocols: ['JMS-KOKO'],
    autoReconnect: {
      retries: 5,
      delay: 3000
    }
  });

  if (!ws.value) {
    globalTipsMessage.error(t('FileListError'));
  }

  initSocketEvent(<WebSocket>ws!.value, t);

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
  downLoadMessage = globalTipsMessage.loading(`Downloading: 0.00%`, { duration: 1000000000 });

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
  sentChunks: Ref<number>
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
    id: uploadFileId.value,
    data: '',
    raw: ''
  };

  const arrayBuffer: ArrayBuffer = await sliceChunk.arrayBuffer();
  const base64String: string = arrayBufferToBase64(arrayBuffer);

  sendData.offSet = sentChunks.value * CHUNK_SIZE;
  sendBody.raw = base64String;
  sendBody.data = JSON.stringify(sendData);

  socket.send(JSON.stringify(sendBody));

  sentChunks.value++;

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
 * @description 中断上传,停止继续发送切片信息
 * @param socket
 * @param fileInfo
 */
const interraptUpload = (socket: WebSocket, fileInfo: UploadFileInfo) => {
  isStop.value = true;
};

/**
 * @description 上传文件
 */
const handleFileUpload = async (
  socket: WebSocket,
  uploadFileList: Ref<Array<UploadFileInfo>>,
  _onProgress: any,
  onFinish: () => void,
  onError: () => void,
  t: any
) => {
  const maxSliceCount = 100;
  const maxChunkSize = 1024 * 1024 * 10;
  const fileManageStore = useFileManageStore();
  const loadingMessage = globalTipsMessage.loading(`${t('UploadProgress')}: 0%`, { duration: 1000000000 });

  let fileInfo = uploadFileList.value[uploadFileList.value.length - 1];

  // 检查是否已存在同名文件
  const existingFiles = new Set(fileManageStore.fileList?.map(file => file.name) || []);

  for (let i = uploadFileList.value.length - 1; i >= 0; i--) {
    const file = uploadFileList.value[i];
    if (!existingFiles.has(file.name)) {
      fileInfo = file;
      break;
    }
  }

  let sliceChunks = [];
  let CHUNK_SIZE = 1024 * 1024 * 5;
  let sentChunks = ref(0);

  watch(
    () => interrupt.value,
    newVal => {
      if (newVal) {
        onError();
        globalTipsMessage.error(t('CancelFileUpload'));
        loadingMessage.destroy();
      }
    }
  );

  const unwatch = watch(
    () => sentChunks.value,
    newValue => {
      const percent = (newValue / sliceChunks.length) * 100;

      _onProgress({ percent });

      loadingMessage.content = `${t('UploadProgress')}: ${Math.floor(percent)}%`;

      if (percent >= 100) {
        onFinish();
        loadingMessage.destroy();
        mittBus.emit('reload-table');
        unwatch();
      }
    }
  );

  if (fileInfo && fileInfo.file) {
    let sliceCount = Math.ceil(fileInfo.file?.size / CHUNK_SIZE);

    // 如果切片数量大于最大切片数量，那么调大切片大小
    if (sliceCount > maxSliceCount) {
      sliceCount = maxSliceCount;
      CHUNK_SIZE = Math.ceil(fileInfo.file?.size / maxSliceCount);
    }

    // 如果切片大小大于最大切片大小，那么依然调整切片数量
    if (CHUNK_SIZE > maxChunkSize) {
      CHUNK_SIZE = maxChunkSize;
      sliceCount = Math.ceil(fileInfo.file?.size / CHUNK_SIZE);
    }

    for (let i = 0; i < sliceCount; i++) {
      sliceChunks.push(fileInfo.file.slice(i * CHUNK_SIZE, (i + 1) * CHUNK_SIZE));
    }

    try {
      uploadFileId.value = Math.floor(Math.random() * Number.MAX_SAFE_INTEGER).toString();

      for (const sliceChunk of sliceChunks) {
        fileManageStore.setReceived(false);

        if (isStop.value) {
          console.log('exec');
          globalTipsMessage.error(t('CancelFileUpload'));
          onError();
          loadingMessage.destroy();
          isStop.value = false;
          return;
        }

        await generateUploadChunks(sliceChunk, socket, fileInfo, CHUNK_SIZE, sentChunks);
      }

      // 结束 chunk 发送 merge: true
      socket.send(
        JSON.stringify({
          cmd: 'upload',
          type: 'SFTP_DATA',
          id: fileManageStore.messageId,
          raw: '',
          data: JSON.stringify({
            offSet: 0,
            merge: true,
            size: 0,
            path: `${fileManageStore.currentPath}/${fileInfo.name}`
          })
        })
      );
      uploadFileId.value = '';
    } catch (e) {
      loadingMessage.destroy();
      onError();
    }
  }
};

/**
 * @description 用于处理文件管理相关逻辑
 */
export const useFileManage = (token: string, t: any) => {
  let fileConnectionUrl: string = `${BASE_WS_URL}/koko/ws/sftp/?token=${token}`;

  function init() {
    const socket = fileSocketConnection(fileConnectionUrl, t);

    mittBus.on(
      'file-upload',
      ({
        uploadFileList,
        onFinish,
        onError,
        onProgress
      }: {
        uploadFileList: Ref<Array<UploadFileInfo>>;
        onFinish: () => void;
        onError: () => void;
        onProgress: (e: { percent: number }) => void;
      }) => {
        handleFileUpload(<WebSocket>socket, uploadFileList, onProgress, onFinish, onError, t);
      }
    );

    mittBus.on('download-file', ({ path, is_dir, size }: { path: string; is_dir: boolean; size: string }) => {
      fileSize = size;
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

    mittBus.on('stop-upload', ({ fileInfo }: { fileInfo: UploadFileInfo }) => {
      interraptUpload(<WebSocket>socket, fileInfo);
    });

    return socket;
  }

  return init();
};

export const unloadListeners = () => {
  mittBus.off('download-file');
  mittBus.off('file-upload');
  mittBus.off('file-manage');
  mittBus.off('stop-upload');
};
