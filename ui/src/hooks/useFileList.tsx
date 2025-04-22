import { v4 as uuid } from 'uuid';
import { useMessage } from 'naive-ui';
import { useWebSocket } from '@vueuse/core';
import { reactive, watchEffect, ref } from 'vue';
import { SFTP_CMD, FILE_MANAGE_MESSAGE_TYPE } from '@/enum';

import type { TreeOption } from 'naive-ui';
import type { RowData } from '@/types/modules/table.type';
import type { IFileManage, IFileManageConnectData, IFileManageSftpFileItem } from '@/hooks/interface';

import {
  Folder,
  Image,
  FileArchive,
  FileVideo,
  FileAudio,
  FileText,
  Code,
  Package,
  FileQuestion
} from 'lucide-vue-next';
import {
  BASE_WS_URL,
  FILE_SUFFIX_CODE,
  FILE_SUFFIX_IMAGE,
  FILE_SUFFIX_VIDEO,
  FILE_SUFFIX_AUDIO,
  FILE_SUFFIX_INSTALL,
  FILE_SUFFIX_DOCUMENT,
  FILE_SUFFIX_COMPRESSION
} from '@/config';

export const useFileList = (token: string, type: 'direct' | 'drawer') => {
  const message = useMessage();
  const sftpUrl = `${BASE_WS_URL}/koko/ws/sftp/?token=${token}`;

  const initial_path = ref('');
  const current_path = ref('');
  const expandedKeys = ref<string[]>([]);
  const socket = ref<WebSocket | null>(null);
  const listData = reactive<RowData[]>([]);
  const treeData = reactive<TreeOption[]>([]);

  /**
   * @description 根据文件名称生成不同类型的文件 icon
   */
  const dispatchFilePrefix = (item: TreeOption) => {
    // 将文件名称通过 . 分割，取最后一个元素
    const fileSuffix = item.label?.split('.').pop()!;

    if (item.is_dir) {
      return (item.prefix = () => <Folder size={18} />);
    }

    if (FILE_SUFFIX_IMAGE.includes(fileSuffix)) {
      return (item.prefix = () => <Image size={18} />);
    }

    if (FILE_SUFFIX_COMPRESSION.includes(fileSuffix)) {
      return (item.prefix = () => <FileArchive size={18} />);
    }

    if (FILE_SUFFIX_VIDEO.includes(fileSuffix)) {
      return (item.prefix = () => <FileVideo size={18} />);
    }

    if (FILE_SUFFIX_AUDIO.includes(fileSuffix)) {
      return (item.prefix = () => <FileAudio size={18} />);
    }

    if (FILE_SUFFIX_DOCUMENT.includes(fileSuffix)) {
      return (item.prefix = () => <FileText size={18} />);
    }

    if (FILE_SUFFIX_CODE.includes(fileSuffix)) {
      return (item.prefix = () => <Code size={18} />);
    }

    if (FILE_SUFFIX_INSTALL.includes(fileSuffix)) {
      return (item.prefix = () => <Package size={18} />);
    }

    item.prefix = () => <FileQuestion size={18} />;
  };

  /**
   * @description 生成抽屉形式的 Table 数据
   */
  const generateTableData = () => {};

  /**
   * @description 异步加载节点
   */
  const handleLoad = (node: TreeOption) => {
    return new Promise<void>(resolve => {
      const sendBody = {
        id: uuid(),
        type: FILE_MANAGE_MESSAGE_TYPE.SFTP_DATA,
        cmd: SFTP_CMD.LIST,
        data: JSON.stringify({ path: node.path })
      };

      console.log(node);

      if (socket.value) {
        socket.value.send(JSON.stringify(sendBody));

        setTimeout(() => {
          resolve();
        }, 300);
      } else {
        resolve();
      }
    });
  };

  /**
   * @description 生成整个页面形式的 Tree 数据
   */
  const generateTreeData = (sftpDataMessageData: IFileManageSftpFileItem[], isRootNode: boolean = true) => {
    // 如果是根节点，则生成完整的树结构
    if (isRootNode) {
      const data: TreeOption[] = [];

      data.push({
        is_dir: true,
        isLeaf: false,
        key: current_path.value,
        label: current_path.value,
        path: current_path.value,
        children: sftpDataMessageData.map(item => {
          const fullPath = `${current_path.value}/${item.name}`;
          return {
            key: fullPath,
            label: item.name,
            is_dir: item.is_dir,
            isLeaf: !item.is_dir,
            path: fullPath
          };
        })
      });

      // 对顶层节点应用图标
      data.forEach((item: TreeOption) => dispatchFilePrefix(item));

      // 对所有子节点应用图标
      data.forEach((item: TreeOption) => {
        if (item.children && Array.isArray(item.children)) {
          item.children.forEach((child: TreeOption) => dispatchFilePrefix(child));
        }
      });

      expandedKeys.value.push(current_path.value);

      return data;
    }
    // 如果不是根节点，只返回子节点数组
    else {
      const children = sftpDataMessageData.map(item => {
        const fullPath = `${current_path.value}/${item.name}`;

        const node = {
          key: fullPath,
          label: item.name,
          is_dir: item.is_dir,
          isLeaf: !item.is_dir,
          path: fullPath
        };

        dispatchFilePrefix(node);

        return node;
      });

      return children;
    }
  };

  const dispatchSFTPCase = (cmdType: string, sftpDataMessageData: IFileManageSftpFileItem[]) => {
    switch (cmdType) {
      case SFTP_CMD.LIST:
        if (initial_path.value === '') {
          initial_path.value = current_path.value;
        }

        listData.length = 0;

        // 直连页面
        if (type === 'direct') {
          // 如果当前路径是根目录或者是初始路径，则不添加 .. 文件夹
          if (current_path.value === '/' || current_path.value === initial_path.value) {
            listData.push(...sftpDataMessageData);
          } else {
            listData.push(
              {
                name: '..',
                size: '',
                perm: '',
                mod_time: '',
                type: '',
                is_dir: true
              },
              ...sftpDataMessageData
            );
          }

          // 生成 Tree 数据
          if (treeData.length === 0) {
            // 首次加载，创建根节点
            treeData.push(...generateTreeData(sftpDataMessageData, true));
          } else {
            // 找到对应路径的节点
            const targetNode = findNodeByPath(treeData);

            if (targetNode) {
              // 更新节点的子节点
              return (targetNode.children = generateTreeData(sftpDataMessageData, false));
            }
          }

          // 对文件列表进行排序：目录在前，文件在后
          listData.sort((a, b) => {
            if (a.name === '..') return -1;
            if (b.name === '..') return 1;

            // 目录在文件前面
            if (a.is_dir && !b.is_dir) return -1;
            if (!a.is_dir && b.is_dir) return 1;

            return a.name.localeCompare(b.name);
          });
        }

        // TODO
        if (type === 'drawer') {
          // 对文件列表进行排序：目录在前，文件在后
          listData.sort((a, b) => {
            // 父目录（..）始终排在最前面
            if (a.name === '..') return -1;
            if (b.name === '..') return 1;

            // 目录在文件前面
            if (a.is_dir && !b.is_dir) return -1;
            if (!a.is_dir && b.is_dir) return 1;

            // 同类型按名称字母顺序排序
            return a.name.localeCompare(b.name);
          });

          return;
        }

        break;
      case SFTP_CMD.MKDIR:
        break;

      case SFTP_CMD.RM:
        break;
    }
  };

  /**
   * @description 递归查找节点
   */
  const findNodeByPath = (nodes: TreeOption[]): TreeOption | null => {
    for (const node of nodes) {
      if (node.path === current_path.value) {
        return node;
      }

      if (node.children && Array.isArray(node.children)) {
        const found = findNodeByPath(node.children);

        if (found) {
          return found;
        }
      }
    }

    return null;
  };

  watchEffect(() => {
    const { ws } = useWebSocket(sftpUrl, {
      protocols: ['JMS-KOKO'],
      autoReconnect: {
        retries: 5,
        delay: 3000
      }
    });

    if (!ws.value) {
      return;
    }

    ws.value.binaryType = 'arraybuffer';
    ws.value.onopen = () => {
      console.log('open');
    };
    ws.value.onerror = event => {
      console.log(event);
    };
    ws.value.onclose = event => {
      console.log(event);
    };
    ws.value.onmessage = event => {
      const transferMessage: IFileManage = JSON.parse(event.data);

      switch (transferMessage.type) {
        case FILE_MANAGE_MESSAGE_TYPE.PING:
          socket.value?.send(
            JSON.stringify({
              id: uuid(),
              type: FILE_MANAGE_MESSAGE_TYPE.PONG,
              data: 'pong'
            })
          );
          break;
        case FILE_MANAGE_MESSAGE_TYPE.PONG:
          break;
        case FILE_MANAGE_MESSAGE_TYPE.ERROR:
          message.error(transferMessage?.err);
          break;
        case FILE_MANAGE_MESSAGE_TYPE.CLOSE:
          ws.value?.close();
          break;
        case FILE_MANAGE_MESSAGE_TYPE.CONNECT:
          const connectMessageData = JSON.parse(transferMessage.data);

          const sendData = {
            path: ''
          };

          const sendBody = {
            id: transferMessage.id,
            type: FILE_MANAGE_MESSAGE_TYPE.SFTP_DATA,
            cmd: SFTP_CMD.LIST,
            data: JSON.stringify(sendData)
          };

          if (connectMessageData) {
            ws.value?.send(JSON.stringify(sendBody));
          }

          break;
        case FILE_MANAGE_MESSAGE_TYPE.SFTP_DATA:
          const cmdType = transferMessage.cmd;
          const sftpDataMessageData: IFileManageSftpFileItem[] = JSON.parse(transferMessage.data);

          current_path.value = transferMessage.current_path;

          dispatchSFTPCase(cmdType, sftpDataMessageData);

          break;
      }
    };

    socket.value = ws.value;
  });

  return {
    socket,
    treeData,
    listData,
    expandedKeys,
    handleLoad
  };
};
