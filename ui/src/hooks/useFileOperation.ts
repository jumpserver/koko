import type { ConfigProviderProps } from 'naive-ui';

import { v4 as uuid } from 'uuid';
import { useI18n } from 'vue-i18n';
import { computed, ref } from 'vue';
import { useWebSocket } from '@vueuse/core';
import { createDiscreteApi, darkTheme } from 'naive-ui';

import type { COMMAND_TYPE } from '@/types/modules/message.type';
import type { FileManage, FileManageSftpFileItem } from '@/types/modules/file.type';

import { createDownloadLink } from '@/utils';
import { BASE_WS_URL } from '@/utils/config';
import { FILE_MANAGE_MESSAGE_TYPE, SFTP_CMD } from '@/types/modules/message.type';

interface sendBody {
  id: string;
  cmd: string;
  type: string;
  data: string;
}

interface sendDataArgs {
  path?: string;
  id?: string;
  filename?: string;
  cmd?: SFTP_CMD;
  type?: FILE_MANAGE_MESSAGE_TYPE;
  sendData?: Record<string, unknown>;
}

type CommandHandler = (message: any) => void;
type CommandMap = Record<COMMAND_TYPE, CommandHandler>;

const OK = 'ok';
const DENIED = 'permission denied';

const configProviderPropsRef = computed<ConfigProviderProps>(() => ({
  theme: darkTheme,
}));

export const useFileOperation = () => {
  const { t } = useI18n();

  const messageId = ref<string>('');
  const initialPath = ref<string>('');
  const currentPath = ref<string>('');
  const fileSocket = ref<WebSocket | null>(null);
  const receivedBuffers = ref<ArrayBuffer[]>([]);
  const fileList = ref<FileManageSftpFileItem[]>([]);

  const { message, notification } = createDiscreteApi(['message', 'notification'], {
    configProviderProps: configProviderPropsRef,
  });

  const commandsDispatch: CommandMap = {
    rm(msg) {
      if (isOk(msg.data)) return opOk();
      if (isDenied(msg.err)) return opDenied();
    },

    list(msg) {
      try {
        let files: FileManageSftpFileItem[] = [];

        if (typeof msg.data === 'string' && msg.data) {
          files = JSON.parse(msg.data);
        }

        // 第一次拿到列表时，记录初始路径
        if (!initialPath.value) {
          initialPath.value = currentPath.value || '/';
        }

        // 只有当不在根路径或初始路径时，才注入 '..'
        const needParent = currentPath.value && currentPath.value !== '/' && currentPath.value !== initialPath.value;

        if (needParent) {
          const hasParent = files.length && files[0]?.name === '..';

          if (!hasParent) {
            files = [
              {
                name: '..',
                size: '',
                perm: '',
                mod_time: '',
                type: '',
                is_dir: true,
              } as FileManageSftpFileItem,
              ...files,
            ];
          }
        }

        fileList.value = files;
      } catch (_) {
        message.error(t('FileListError'));
        fileList.value = [];
      }
    },

    mkdir(msg) {
      if (isOk(msg.data)) return opOk();
    },

    rename(msg) {
      if (isOk(msg.data)) return opOk();
    },

    upload(msg) {
      if (isOk(msg.data)) return opOk('UploadSuccess');
      if (isDenied(msg.err)) return opDenied('UploadFailed');
    },

    download(msg) {
      if (msg.data) return createDownloadLink(receivedBuffers.value, message);
      if (isDenied(msg.err)) return opDenied('DownloadFailed');
    },
  };

  function isOk(s?: string | null) {
    return (s ?? '').toLowerCase() === OK;
  }

  function isDenied(s?: string | null) {
    return (s ?? '').toLowerCase() === DENIED;
  }

  function opOk(tipMessage?: string) {
    message.success(t(tipMessage ?? 'OperationSuccessful'));
  }

  function opDenied(tipMessage?: string) {
    message.error(t(tipMessage ?? 'PermissionDenied'));
  }

  /**
   * @description 构造消息体
   */
  const createSftpMessage = ({
    path = '',
    id = uuid(),
    cmd = SFTP_CMD.LIST,
    type = FILE_MANAGE_MESSAGE_TYPE.SFTP_DATA,
    sendData = {},
  }: sendDataArgs): sendBody => {
    const payload = {
      path,
      ...sendData,
    };

    const sendBody: sendBody = {
      id,
      cmd,
      type,
      data: JSON.stringify(payload),
    };

    return sendBody;
  };

  /**
   * @description 重命名
   */
  const handleFileRename = (path: string, filename: string) => {
    const sendBody = createSftpMessage({
      id: uuid(),
      path,
      cmd: SFTP_CMD.RENAME,
      sendData: {
        new_name: filename,
      },
    });

    fileSocket.value?.send(JSON.stringify(sendBody));
  };

  /**
   * @description 移除文件
   */
  const handleFileRemove = (path: string) => {
    const sendBody = createSftpMessage({
      id: uuid(),
      path,
      cmd: SFTP_CMD.RM,
    });

    fileSocket.value?.send(JSON.stringify(sendBody));
  };

  /**
   * @description 下载文件
   */
  const handleFileDownload = () => {};

  /**
   * @description 创建文件夹
   */
  const handleCreateFolder = (path: string) => {
    const sendBody = createSftpMessage({
      id: uuid(),
      path,
      cmd: SFTP_CMD.MKDIR,
    });

    fileSocket.value?.send(JSON.stringify(sendBody));
  };

  /**
   * @description 刷新文件列表
   */
  const handleRefreshFileList = (path: string) => {
    const sendBody = createSftpMessage({
      id: uuid(),
      path,
    });

    fileSocket.value?.send(JSON.stringify(sendBody));
  };

  /**
   * @description 处理文件消息
   * @param {MessageEvent} event
   */
  const handleFileMessage = (event: MessageEvent) => {
    const messageData: FileManage = JSON.parse(event.data);

    // fileManageStore.setMessageId(message.id);
    // fileManageStore.setCurrentPath(message.current_path);
    messageId.value = messageData.id;
    currentPath.value = messageData.current_path;

    switch (messageData.type) {
      case FILE_MANAGE_MESSAGE_TYPE.CONNECT: {
        const sendBody = createSftpMessage({
          id: messageData.id,
        });

        fileSocket.value?.send(JSON.stringify(sendBody));

        break;
      }

      case FILE_MANAGE_MESSAGE_TYPE.CLOSE: {
        break;
      }

      case FILE_MANAGE_MESSAGE_TYPE.ERROR: {
        message.error(messageData.err ? messageData.err : t('FileListError'));
        break;
      }

      case FILE_MANAGE_MESSAGE_TYPE.PING: {
        fileSocket.value?.send(
          JSON.stringify({
            id: uuid(),
            type: FILE_MANAGE_MESSAGE_TYPE.PONG,
            data: 'pong',
          })
        );
        break;
      }

      case FILE_MANAGE_MESSAGE_TYPE.PONG: {
        break;
      }

      case FILE_MANAGE_MESSAGE_TYPE.SFTP_DATA: {
        const handler = commandsDispatch[messageData.cmd as keyof typeof commandsDispatch];

        if (handler) {
          handler(messageData);
        }
        break;
      }

      // 在 SFTP_BINARY 中只会传输数据进行,真正的下载行为需要等到 SFTP_DATA 的 download
      case FILE_MANAGE_MESSAGE_TYPE.SFTP_BINARY: {
        try {
          const binaryString = atob(messageData.raw);
          const bytes = Uint8Array.from(binaryString, c => c.charCodeAt(0));

          receivedBuffers.value.push(bytes);
        } catch (_) {
          // TODO 翻译
          message.error(t('SFTP_BINARY 解码失败'));
        }
        break;
      }

      default: {
        break;
      }
    }
  };

  /**
   * @description 创建文件 socket
   */
  const createFileSocket = (token: string) => {
    const connectionUrl = `${BASE_WS_URL}/koko/ws/sftp/?token=${token}`;

    const { ws } = useWebSocket(connectionUrl, {
      protocols: ['JMS-KOKO'],
      autoReconnect: {
        retries: 5,
        delay: 3000,
      },
    });

    if (!ws.value) return message.error(t('FileListError'));

    ws.value.binaryType = 'arraybuffer';
    ws.value.close = () => {};
    ws.value.onopen = () => {};
    ws.value.onmessage = (event: MessageEvent) => handleFileMessage(event);

    fileSocket.value = ws.value;
  };

  return {
    fileList,
    fileSocket,
    currentPath,
    initialPath,

    createFileSocket,
    handleFileRename,
    handleFileRemove,
    handleFileDownload,
    handleCreateFolder,
    handleRefreshFileList,
  };
};
