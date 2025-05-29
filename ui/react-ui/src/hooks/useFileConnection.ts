import { message } from 'antd';
import { v4 as uuid } from 'uuid';
import { useRef, useState } from 'react';
import { useFileStatus } from '@/store/useFileStatus';
import { getConnectionUrl, arrayBufferToBase64 } from '@/utils';
import { FILE_MANAGE_MESSAGE_TYPE, SFTP_CMD, FILE_OPERATION_TYPE } from '@/enums';

import type { RcFile } from 'antd/es/upload/interface';
import type { FileMessage, FileSendData, UploadFileItem } from '@/types/file.type';

// 最大切片数量
const MAX_SLICE_COUNT = 100;

export const useFileConnection = () => {
  const [initialPath, setInitialPath] = useState<string>('');
  const [spinning, setSpinning] = useState(false);
  const [currentUploadMessage, setCurrentUploadMessage] = useState<UploadFileItem | null>(null);

  const socket = useRef<WebSocket | null>(null);
  const chunkReceived = useRef<boolean>(false);
  const messageId = useRef<string>('');
  const currentPath = useRef<string>('');

  const { setFileMessage, setUploadFileList } = useFileStatus();

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
      case SFTP_CMD.RM:
        setSpinning(false);
        if (fileMessage.data === 'ok') {
          message.success('删除文件夹成功');
          handleFileOperation(FILE_OPERATION_TYPE.REFRESH);
        }
        break;
      case SFTP_CMD.LIST:
        {
          setSpinning(false);
          currentPath.current = current_path;

          setFileMessage({
            paths: current_path.split('/').filter(item => item !== '/' && item !== ''),
            fileList: JSON.parse(data)
          });
        }

        break;
      case SFTP_CMD.MKDIR:
        setSpinning(false);
        if (fileMessage.data === 'ok') {
          message.success('创建文件夹成功');
          handleFileOperation(FILE_OPERATION_TYPE.REFRESH);
        }
        break;
      case SFTP_CMD.MKFILE:
        setSpinning(false);
        break;
      case SFTP_CMD.RENAME:
        setSpinning(false);
        if (fileMessage.data === 'ok') {
          message.success('重命名成功');
          handleFileOperation(FILE_OPERATION_TYPE.REFRESH);
        }
        break;
      case SFTP_CMD.UPLOAD:
        chunkReceived.current = true;
    }
  };

  /**
   * @description 统一处理文件操作
   * @param operationType 操作类型
   * @param path 操作的文件/文件夹路径（相对于当前路径）
   */
  const handleFileOperation = (operationType: FILE_OPERATION_TYPE, path?: string, newName?: string) => {
    let sendBody: any;

    setSpinning(true);

    switch (operationType) {
      case FILE_OPERATION_TYPE.RENAME:
        if (!path) {
          console.error('重命名操作需要提供文件路径');
          setSpinning(false);
          return;
        }

        sendBody = {
          id: uuid(),
          type: FILE_MANAGE_MESSAGE_TYPE.SFTP_DATA,
          cmd: SFTP_CMD.RENAME,
          data: JSON.stringify({ path: `${currentPath.current}/${path}`, new_name: newName })
        };

        break;

      case FILE_OPERATION_TYPE.DELETE:
        if (!path) {
          console.error('删除操作需要提供文件路径');
          setSpinning(false);
          return;
        }
        sendBody = {
          id: uuid(),
          type: FILE_MANAGE_MESSAGE_TYPE.SFTP_DATA,
          cmd: SFTP_CMD.RM,
          data: JSON.stringify({ path: `${currentPath.current}/${path}` })
        };
        break;

      case FILE_OPERATION_TYPE.REFRESH:
        sendBody = {
          id: uuid(),
          cmd: SFTP_CMD.LIST,
          type: FILE_MANAGE_MESSAGE_TYPE.SFTP_DATA,
          data: JSON.stringify({
            path: currentPath.current
          })
        };
        break;

      case FILE_OPERATION_TYPE.OPEN_FOLDER:
        // 进入文件与返回到上一层级类似，只不过是路径不同
        if (!path) {
          sendBody = {
            id: uuid(),
            type: FILE_MANAGE_MESSAGE_TYPE.SFTP_DATA,
            cmd: SFTP_CMD.LIST,
            data: JSON.stringify({ path: currentPath.current.split('/').slice(0, -1).join('/') })
          };
          break;
        }

        sendBody = {
          id: uuid(),
          type: FILE_MANAGE_MESSAGE_TYPE.SFTP_DATA,
          cmd: SFTP_CMD.LIST,
          data: JSON.stringify({
            path: `${currentPath.current}/${path}`
          })
        };
        break;

      case FILE_OPERATION_TYPE.CREATE_FOLDER:
        if (!path) {
          console.error('创建文件夹操作需要提供文件夹名称');
          setSpinning(false);
          return;
        }
        sendBody = {
          id: uuid(),
          type: FILE_MANAGE_MESSAGE_TYPE.SFTP_DATA,
          cmd: SFTP_CMD.MKDIR,
          data: JSON.stringify({
            path: currentPath.current + '/' + path
          })
        };
        break;

      default:
        console.error('未知的文件操作类型:', operationType);
        setSpinning(false);
        return;
    }

    socket.current?.send(JSON.stringify(sendBody));
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
   * @description 处理文件上传
   * @param file 文件
   */
  const handleFileUpload = async (file: RcFile) => {
    const sendBody = {
      raw: '',
      data: '',
      id: Math.floor(Math.random() * Number.MAX_SAFE_INTEGER).toString(),
      cmd: SFTP_CMD.UPLOAD,
      type: FILE_MANAGE_MESSAGE_TYPE.SFTP_DATA
    };

    const sendData: FileSendData = {
      offSet: 0,
      size: file.size,
      path: `${currentPath.current}/${file.name}`
    };

    let chunkSize = 1024 * 1024 * 5;
    let sliceCount = Math.ceil(file.size / chunkSize);

    if (sliceCount > MAX_SLICE_COUNT) {
      // 动态将 chunkSize 扩容 10%
      sliceCount = MAX_SLICE_COUNT;
      chunkSize = chunkSize * 1.1;
    }

    setCurrentUploadMessage({
      filename: file.name,
      totalSize: file.size,
      status: 'uploading',
      md5: '',
      uploaded: 0
    });

    try {
      for (let i = 0; i < sliceCount; i++) {
        const chunk = file.slice(i * chunkSize, (i + 1) * chunkSize);
        const arrayBuffer = await chunk.arrayBuffer();
        const base64String: string = arrayBufferToBase64(arrayBuffer);

        sendData.offSet = i * chunkSize;
        sendBody.raw = base64String;
        sendBody.data = JSON.stringify(sendData);

        // 重置确认状态
        chunkReceived.current = false;

        // 发送当前切片
        socket.current?.send(JSON.stringify(sendBody));

        // 等待当前切片的确认
        await new Promise<void>(resolve => {
          const checkReceived = () => {
            console.log('chunkReceived', chunkReceived.current);
            if (chunkReceived.current) {
              resolve();
            } else {
              setTimeout(checkReceived, 100);
            }
          };
          checkReceived();
        });

        // 更新进度
        const uploadedBytes = (i + 1) * chunkSize > file.size ? file.size : (i + 1) * chunkSize;

        setCurrentUploadMessage(prev => {
          if (prev) {
            return {
              ...prev,
              uploaded: uploadedBytes
            };
          }
          return prev;
        });
      }

      // 所有切片上传完成
      setCurrentUploadMessage(prev => {
        if (prev) {
          return {
            ...prev,
            status: 'success',
            uploaded: file.size
          };
        }
        return prev;
      });
    } catch (e) {
      message.error(String(e));
      setCurrentUploadMessage(prev => {
        if (prev) {
          return {
            ...prev,
            status: 'error'
          };
        }
        return prev;
      });
    }
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

    ws.onerror = () => {
      setSpinning(false);
      message.error('SFTP 连接错误');
    };

    ws.onclose = () => {
      setSpinning(false);
      message.error('SFTP connection has been closed');
    };

    ws.onmessage = (message: MessageEvent) => {
      handleFileConnectEvent(message);
    };
  };

  return {
    spinning,
    currentUploadMessage,
    handleFileUpload,
    createFileSocket,
    handleFileOperation
  };
};
