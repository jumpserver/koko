import type { ConfigProviderProps, UploadFileInfo } from 'naive-ui';
import type { MessageApiInjection } from 'naive-ui/es/message/src/MessageProvider';

import { computed } from 'vue';
import { v4 as uuid } from 'uuid';
import { useWebSocket } from '@vueuse/core';
import { createDiscreteApi, darkTheme } from 'naive-ui';

import type {
  FileManage,
  FileManageConnectData,
  FileManageSftpFileItem,
  FileSendData,
} from '@/types/modules/file.type';

import mittBus from '@/utils/mittBus';
import { BASE_WS_URL } from '@/utils/config';
import { lunaCommunicator } from '@/utils/lunaBus';
import { LUNA_MESSAGE_TYPE } from '@/types/modules/message.type';
import { useFileManageStore } from '@/store/modules/fileManage.ts';

export enum MessageType {
  CONNECT = 'CONNECT',
  CLOSE = 'CLOSE',
  ERROR = 'ERROR',
  PING = 'PING',
  PONG = 'PONG',
  CLOSED = 'closed',
  SFTP_DATA = 'SFTP_DATA',
  SFTP_BINARY = 'SFTP_BINARY',
}
export enum ManageTypes {
  CREATE = 'CREATE',
  CHANGE = 'CHANGE',
  REFRESH = 'REFRESH',
  RENAME = 'RENAME',
  REMOVE = 'REMOVE',
}

const configProviderPropsRef = computed<ConfigProviderProps>(() => ({
  theme: darkTheme,
}));
const { message: globalTipsMessage }: { message: MessageApiInjection } = createDiscreteApi(['message'], {
  configProviderProps: configProviderPropsRef,
});

interface DownloadTaskState {
  buffers: Uint8Array[];
  fileSize: number;
  message: any;
}

interface UploadTaskState {
  abortType: 'connection' | 'manual' | 'server' | null;
  errorMessage?: string;
  fileId: string;
  fileInfo: UploadFileInfo;
  requestId: string;
  resolveAck?: (result: boolean) => void;
  suppressErrorMessage?: boolean;
}

// TODO 都是 hook 内部状态
let initialPath = '';
const downloadTasks = new Map<string, DownloadTaskState>();
const uploadTasks = new Map<string, UploadTaskState>();
let uploadSuccessCount = 0;

function flushUploadSuccess(t: any) {
  if (uploadSuccessCount === 0 || uploadTasks.size > 0) {
    return;
  }
  const content
    = uploadSuccessCount === 1 ? t('UploadSuccess') : `${t('UploadSuccess')} (${uploadSuccessCount})`;

  uploadSuccessCount = 0;
  globalTipsMessage.success(content);
}

function failActiveUploads(message: string) {
  const activeTasks = Array.from(uploadTasks.values()).filter(task => task.abortType == null);

  if (activeTasks.length === 0) {
    return false;
  }

  globalTipsMessage.error(message);

  activeTasks.forEach((task) => {
    task.abortType = 'connection';
    task.errorMessage = message;
    task.suppressErrorMessage = true;
    task.resolveAck?.(false);
    task.resolveAck = undefined;
  });

  return true;
}

/**
 * @description 将 buffer 转为 base64
 * @param buffer
 */
function arrayBufferToBase64(buffer: ArrayBuffer): string {
  const uint8Array = new Uint8Array(buffer);
  const CHUNK_SIZE = 0x8000;

  let result = '';

  for (let i = 0; i < uint8Array.length; i += CHUNK_SIZE) {
    const chunk = uint8Array.subarray(i, i + CHUNK_SIZE);
    result += String.fromCharCode.apply(null, chunk as unknown as number[]);
  }

  return btoa(result);
}

/**
 * @description 刷新文件列表
 * @param socket
 * @param path
 */
export function refresh(socket: WebSocket, path: string) {
  const sendData = {
    path,
  };

  const sendBody = {
    id: uuid(),
    cmd: 'list',
    type: 'SFTP_DATA',
    data: JSON.stringify(sendData),
  };

  socket.send(JSON.stringify(sendBody));
}

/**
 * @description 处理 type 为 connect 的方法
 * @param messageData
 * @param id
 * @param socket
 */
function handleSocketConnectEvent(messageData: FileManageConnectData, id: string, socket: WebSocket) {
  const sendData = {
    path: '',
  };

  const sendBody = {
    id,
    type: 'SFTP_DATA',
    cmd: 'list',
    data: JSON.stringify(sendData),
  };

  if (messageData) {
    socket.send(JSON.stringify(sendBody));
  }
}

function isPermissionDenied(err: string) {
  return err.toLowerCase() === 'permission denied';
}

/**
 * @description mkdir / rm / rename 无论成功失败都刷新列表，避免 loading 卡住
 */
function handleFileActionResult(message: FileManage, t: any) {
  if (message.err) {
    globalTipsMessage.error(isPermissionDenied(message.err) ? t('PermissionDenied') : message.err);
  }
  else if (message.data === 'ok') {
    globalTipsMessage.success(t('OperationSuccessful'));
  }

  mittBus.emit('reload-table');
}

function handleDownloadResult(message: FileManage, t: any) {
  const downloadTask = downloadTasks.get(message.id);

  if (message.err) {
    downloadTask?.message.destroy();
    downloadTasks.delete(message.id);
    globalTipsMessage.error(isPermissionDenied(message.err) ? t('PermissionDenied') : message.err);
    return;
  }

  if (!message.data || !downloadTask) {
    return;
  }

  const blob: Blob = new Blob(downloadTask.buffers, {
    type: 'application/octet-stream',
  });

  const url = window.URL.createObjectURL(blob);
  const a = document.createElement('a');

  a.style.display = 'none';
  a.href = url;
  a.download = message.data;

  document.body.appendChild(a);
  a.click();

  window.URL.revokeObjectURL(url);
  document.body.removeChild(a);
  downloadTask.message.destroy();
  downloadTasks.delete(message.id);
}

/**
 * @description 设置文件信息 table
 * @param messageData
 */
function handleSocketSftpData(messageData: FileManageSftpFileItem[]) {
  const fileManageStore = useFileManageStore();

  // 初始化时保存初始路径
  if (initialPath === '') {
    initialPath = fileManageStore.currentPath;
  }

  // 如果当前路径是根目录或者是初始路径，则不添加 .. 文件夹
  if (fileManageStore.currentPath === '/' || fileManageStore.currentPath === initialPath) {
    messageData = [...messageData];
  }
  else {
    messageData = [
      {
        name: '..',
        size: '',
        perm: '',
        mod_time: '',
        type: '',
        is_dir: true,
      },
      ...messageData,
    ];
  }

  fileManageStore.setFileList(messageData);
}

/**
 * @description 心跳检测机制
 * @param socket WebSocket实例
 */
function heartBeat(socket: WebSocket) {
  let pingInterval: number | null = null;

  const sendPing = () => {
    if (socket.CLOSED === socket.readyState || socket.CLOSING === socket.readyState) {
      clearInterval(pingInterval!);
      return;
    }

    const pingMessage = {
      id: uuid(),
      type: MessageType.PING,
      data: 'ping',
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
}

/**
 * @description 处理 message
 * @param socket
 */
function initSocketEvent(socket: WebSocket, t: any) {
  const fileManageStore = useFileManageStore();

  let clearHeartbeat: (() => void) | null = null;

  socket.binaryType = 'arraybuffer';

  socket.onopen = () => {
    clearHeartbeat = heartBeat(socket);
  };
  socket.onerror = () => {
    clearHeartbeat?.();
    failActiveUploads(t('WebSocketError'));
  };
  socket.onclose = () => {
    clearHeartbeat?.();
    failActiveUploads(t('WebSocketClosed'));
  };

  socket.onmessage = (event: MessageEvent) => {
    let message: FileManage;

    try {
      message = JSON.parse(event.data);
    }
    catch (error) {
      console.error(error);
      fileManageStore.setFileList([...(fileManageStore.fileList || [])]);
      globalTipsMessage.error(t('FileListError'));
      return;
    }

    fileManageStore.setCurrentPath(message.current_path);

    switch (message.type) {
      case MessageType.CONNECT: {
        handleSocketConnectEvent(JSON.parse(message.data), message.id, socket);
        break;
      }

      case MessageType.SFTP_DATA: {
        const uploadTask = uploadTasks.get(message.id);

        if (uploadTask && message.cmd === 'upload') {
          if (message.err || message.data === 'No permission') {
            uploadTask.abortType = 'server';
            uploadTask.errorMessage = message.err || t('NoPermission');
          }
          uploadTask.resolveAck?.(uploadTask.abortType == null);
          uploadTask.resolveAck = undefined;
          break;
        }

        if (message.cmd === 'mkdir' || message.cmd === 'rm' || message.cmd === 'rename') {
          handleFileActionResult(message, t);
          break;
        }

        if (message.cmd === 'download') {
          handleDownloadResult(message, t);
          break;
        }

        if (message.cmd === 'list') {
          try {
            const payload = message.data ? JSON.parse(message.data) : [];
            handleSocketSftpData(Array.isArray(payload) ? payload : []);
          }
          catch (error) {
            console.error(error);
            fileManageStore.setFileList([]);
            globalTipsMessage.error(t('FileListError'));
          }
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

        const downloadTask = downloadTasks.get(message.id);

        if (!downloadTask) {
          break;
        }

        downloadTask.buffers.push(bytes);

        let receivedBytes = 0;

        for (const buffer of downloadTask.buffers) {
          receivedBytes += buffer.length;
        }

        const percent = downloadTask.fileSize > 0 ? Math.min((receivedBytes / downloadTask.fileSize) * 100, 100) : 100;
        downloadTask.message.content = `${t('DownloadProgress')}: ${percent.toFixed(2)}%`;

        break;
      }

      case MessageType.ERROR: {
        const uploadTask = uploadTasks.get(message.id);

        if (uploadTask) {
          uploadTask.abortType = 'server';
          uploadTask.errorMessage = message.err || t('FileListError');
          uploadTask.resolveAck?.(false);
          uploadTask.resolveAck = undefined;
          break;
        }

        const downloadTask = downloadTasks.get(message.id);

        if (downloadTask) {
          downloadTask.message.destroy();
          downloadTasks.delete(message.id);
          globalTipsMessage.error(message.err ? message.err : t('FileListError'));
          break;
        }
        if (message.cmd === 'list') {
          fileManageStore.setFileList([]);
        }
        if (message.cmd === 'rm' || message.cmd === 'rename' || message.cmd === 'mkdir') {
          mittBus.emit('reload-table');
        }
        globalTipsMessage.error(message.err ? message.err : t('FileListError'));
        break;
      }

      case MessageType.PING: {
        socket.send(
          JSON.stringify({
            id: uuid(),
            type: MessageType.PONG,
            data: 'pong',
          }),
        );
        break;
      }

      case MessageType.PONG: {
        break;
      }

      case MessageType.CLOSE: {
        if (!failActiveUploads(t('FileManagementExpired'))) {
          globalTipsMessage.error(t('FileManagementExpired'));
        }
        downloadTasks.forEach(task => task.message.destroy());
        downloadTasks.clear();

        // 文件列表置空
        fileManageStore.setFileList([]);
        // 文件路径置空
        fileManageStore.setCurrentPath('');

        socket.close();
        lunaCommunicator.sendLuna(LUNA_MESSAGE_TYPE.FILE_MANAGE_EXPIRED, '');

        mittBus.emit('file-manager-expired');
        break;
      }

      default: {
        break;
      }
    }
  };
}

/**
 * @description 文件管理中的 Socket 连接
 * @param url
 */
function fileSocketConnection(url: string, t: any) {
  const { ws } = useWebSocket(url, {
    protocols: ['JMS-KOKO'],
    autoReconnect: {
      retries: 5,
      delay: 3000,
    },
  });

  if (!ws.value) {
    globalTipsMessage.error(t('FileListError'));
  }

  initSocketEvent(<WebSocket>ws!.value, t);

  return ws.value;
}

/**
 * @description 路径跳转的处理
 * @param socket
 * @param path
 */
function handleChangePath(socket: WebSocket, path: string) {
  const sendBody = {
    id: uuid(),
    type: 'SFTP_DATA',
    cmd: 'list',
    data: JSON.stringify({ path }),
  };

  socket.send(JSON.stringify(sendBody));
}

/**
 * @description 创建文件夹
 * @param socket
 * @param path
 */
function handleFileCreate(socket: WebSocket, path: string) {
  const sendBody = {
    id: uuid(),
    type: 'SFTP_DATA',
    cmd: 'mkdir',
    data: JSON.stringify({ path }),
  };

  socket.send(JSON.stringify(sendBody));
}

/**
 * @description 重命名
 * @param socket
 * @param path
 * @param newName
 */
function handleFileRename(socket: WebSocket, path: string, newName: string) {
  const sendBody = {
    id: uuid(),
    type: 'SFTP_DATA',
    cmd: 'rename',
    data: JSON.stringify({ path, new_name: newName }),
  };

  socket.send(JSON.stringify(sendBody));
}

/**
 * @description 移除文件
 * @param socket
 * @param path
 */
function handleFileRemove(socket: WebSocket, path: string) {
  const sendBody = {
    id: uuid(),
    type: 'SFTP_DATA',
    cmd: 'rm',
    data: JSON.stringify({ path }),
  };

  socket.send(JSON.stringify(sendBody));
}

/**
 * @description 下载文件
 * @param socket
 * @param path
 * @param is_dir
 */
function handleFileDownload(socket: WebSocket, path: string, is_dir: boolean, size: string, t: any) {
  const requestId = uuid();

  downloadTasks.set(requestId, {
    buffers: [],
    fileSize: Number(size) || 0,
    message: globalTipsMessage.loading(`${t('DownloadProgress')}: 0.00%`, { duration: 1000000000 }),
  });

  const sendData = {
    path,
    is_dir,
  };

  const sendBody = {
    id: requestId,
    type: 'SFTP_DATA',
    cmd: 'download',
    data: JSON.stringify(sendData),
  };

  socket.send(JSON.stringify(sendBody));
}

/**
 * @description 预处理 chunks
 */
async function generateUploadChunks(
  sliceChunk: Blob,
  socket: WebSocket,
  uploadTask: UploadTaskState,
  CHUNK_SIZE: number,
  sentChunks: number,
  isSingleChunk: boolean = false,
) {
  const fileManageStore = useFileManageStore();
  const sendData: FileSendData = {
    offSet: 0,
    size: uploadTask.fileInfo.file?.size,
    path: `${fileManageStore.currentPath}/${uploadTask.fileInfo.name}`,
  };

  if (isSingleChunk) {
    sendData.chunk = false;
  }
  else {
    sendData.merge = isSingleChunk;
    sendData.chunk = !isSingleChunk;
  }

  const sendBody = {
    cmd: 'upload',
    type: 'SFTP_DATA',
    id: uploadTask.requestId,
    data: '',
    raw: '',
  };

  try {
    const arrayBuffer: ArrayBuffer = await sliceChunk.arrayBuffer();
    const base64String: string = arrayBufferToBase64(arrayBuffer);

    sendData.offSet = sentChunks * CHUNK_SIZE;
    sendBody.raw = base64String;
    sendBody.data = JSON.stringify(sendData);

    return new Promise<boolean>((resolve) => {
      uploadTask.resolveAck = resolve;
      socket.send(JSON.stringify(sendBody));
    });
  }
  catch (error) {
    uploadTask.abortType = 'connection';
    uploadTask.errorMessage = error instanceof Error ? error.message : String(error);
    console.error(error);
    return false;
  }
}

function mergeUploadChunks(socket: WebSocket, uploadTask: UploadTaskState, path: string) {
  return new Promise<boolean>((resolve) => {
    uploadTask.resolveAck = resolve;
    socket.send(
      JSON.stringify({
        cmd: 'upload',
        type: 'SFTP_DATA',
        id: uploadTask.requestId,
        raw: '',
        data: JSON.stringify({
          offSet: 0,
          merge: true,
          size: 0,
          path,
        }),
      }),
    );
  });
}

function finishUploadWithError(uploadTask: UploadTaskState, onError: () => void, t: any) {
  if (uploadTask.abortType === 'manual') {
    globalTipsMessage.error(t('CancelFileUpload'));
    mittBus.emit('upload-stopped', { fileId: uploadTask.fileId });
  }
  else if (!uploadTask.suppressErrorMessage) {
    globalTipsMessage.error(`${uploadTask.fileInfo.name}: ${uploadTask.errorMessage || t('FileListError')}`);
  }

  onError();
  uploadTasks.delete(uploadTask.requestId);
  flushUploadSuccess(t);
}

/**
 * @description 中断上传,停止继续发送切片信息
 */
function interraptUpload(fileInfo: UploadFileInfo) {
  const uploadTask = Array.from(uploadTasks.values()).find(task => task.fileId === fileInfo.id);

  if (!uploadTask) {
    return;
  }

  uploadTask.abortType = 'manual';
  uploadTask.resolveAck?.(false);
  uploadTask.resolveAck = undefined;
}

/**
 * @description 上传文件
 */
async function handleFileUpload(
  socket: WebSocket,
  fileInfo: UploadFileInfo,
  _onProgress: any,
  onFinish: () => void,
  onError: () => void,
  t: any,
) {
  const maxSliceCount = 100;
  const maxChunkSize = 1024 * 1024 * 10;
  const fileManageStore = useFileManageStore();

  const sliceChunks = [];
  let CHUNK_SIZE = 1024 * 1024 * 5;
  let sentChunks = 0;
  const requestId = Math.floor(Math.random() * Number.MAX_SAFE_INTEGER).toString();
  const uploadTask: UploadTaskState = {
    abortType: null,
    fileId: fileInfo.id,
    fileInfo,
    requestId,
  };

  if (fileInfo && fileInfo.file) {
    uploadTasks.set(requestId, uploadTask);
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
      // 判断是否只有一个切片
      const isSingleChunk = sliceChunks.length === 1;

      for (const sliceChunk of sliceChunks) {
        if (uploadTask.abortType) {
          finishUploadWithError(uploadTask, onError, t);
          return;
        }

        const result = await generateUploadChunks(
          sliceChunk,
          socket,
          uploadTask,
          CHUNK_SIZE,
          sentChunks,
          isSingleChunk,
        );

        if (!result) {
          finishUploadWithError(uploadTask, onError, t);
          return;
        }

        sentChunks++;
        _onProgress({ percent: (sentChunks / sliceChunks.length) * 100 });
      }

      if (uploadTask.abortType) {
        finishUploadWithError(uploadTask, onError, t);
        return;
      }

      // 如果不是单切片，才需要发送merge请求
      if (sliceChunks.length > 1) {
        const merged = await mergeUploadChunks(
          socket,
          uploadTask,
          `${fileManageStore.currentPath}/${fileInfo.name}`,
        );

        if (!merged) {
          finishUploadWithError(uploadTask, onError, t);
          return;
        }
      }

      onFinish();
      uploadSuccessCount++;
      uploadTasks.delete(requestId);
      flushUploadSuccess(t);
      mittBus.emit('reload-table');
    }
    catch (e) {
      uploadTask.abortType = 'connection';
      uploadTask.errorMessage = e instanceof Error ? e.message : String(e);
      console.error(e);
      finishUploadWithError(uploadTask, onError, t);
    }
  }
}

/**
 * @description 用于处理文件管理相关逻辑
 */
export function useFileManage(token: string, t: any) {
  const fileConnectionUrl: string = `${BASE_WS_URL}/koko/ws/sftp/?token=${token}`;

  function init() {
    const socket = fileSocketConnection(fileConnectionUrl, t);

    mittBus.on(
      'file-upload',
      ({
        fileInfo,
        onFinish,
        onError,
        onProgress,
      }: {
        fileInfo: UploadFileInfo;
        onFinish: () => void;
        onError: () => void;
        onProgress: (e: { percent: number }) => void;
      }) => {
        handleFileUpload(<WebSocket>socket, fileInfo, onProgress, onFinish, onError, t);
      },
    );

    mittBus.on('download-file', ({ path, is_dir, size }: { path: string; is_dir: boolean; size: string }) => {
      handleFileDownload(<WebSocket>socket, path, is_dir, size, t);
    });

    mittBus.on('file-manage', ({ path, type, new_name }: { path: string; type: ManageTypes; new_name?: string }) => {
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
    });

    mittBus.on('stop-upload', (data: { fileInfo: UploadFileInfo }) => {
      interraptUpload(data.fileInfo);
    });

    return socket;
  }

  return init();
}

export function unloadListeners() {
  mittBus.off('download-file');
  mittBus.off('file-upload');
  mittBus.off('file-manage');
  mittBus.off('stop-upload');
  mittBus.off('upload-stopped');
}
