import { v4 as uuid } from 'uuid';
import { useWebSocket } from '@vueuse/core';
import { useMessage, NEllipsis } from 'naive-ui';
import { reactive, watchEffect, ref, Ref, watch } from 'vue';
import { SFTP_CMD, FILE_MANAGE_MESSAGE_TYPE } from '@/enum';

import type { TreeOption } from 'naive-ui';
import type { IFileManage, IFileManageConnectData, IFileManageSftpFileItem } from '@/hooks/interface';

import { Folder, Image, FileArchive, FileVideo, FileAudio, FileText, Code, Package } from 'lucide-vue-next';
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

export const useFileList = (token: string) => {
  const message = useMessage();
  const sftpUrl = `${BASE_WS_URL}/koko/ws/sftp/?token=${token}`;

  const socket = ref<WebSocket | null>(null);
  const treeData = reactive<TreeOption[]>([]);

  const renderLabel = (splitValue: Ref<number>) => {
    const maxWidth = ref('190px');

    // TODO 再做调整
    watch(
      () => splitValue.value,
      value => {
        // 计算最大宽度：基础宽度加上根据 splitValue 计算的额外宽度
        const baseWidth = 70;
        const extraWidth = Math.round(1200 * value);
        maxWidth.value = `${baseWidth + extraWidth}px`;
      },
      { immediate: true }
    );

    return ({ option }: { option: TreeOption }) => {
      return (
        <NEllipsis style={{ maxWidth: maxWidth.value }}>
          <span class="font-medium text-sm font-PingFangSC text-ellipsis overflow-hidden">{option.label}</span>
        </NEllipsis>
      );
    };
  };

  /**
   * @description 根据文件名称生成不同类型的文件 icon
   */
  const dispatchFilePrefix = (item: TreeOption) => {
    // 将文件名称通过 . 分割，取最后一个元素
    const fileSuffix = item.label?.split('.').pop()!;

    if (item.is_dir) {
      item.prefix = () => <Folder size={18} />;
    }

    if (FILE_SUFFIX_IMAGE.includes(fileSuffix)) {
      item.prefix = () => <Image size={18} />;
    }

    if (FILE_SUFFIX_COMPRESSION.includes(fileSuffix)) {
      item.prefix = () => <FileArchive size={18} />;
    }

    if (FILE_SUFFIX_VIDEO.includes(fileSuffix)) {
      item.prefix = () => <FileVideo size={18} />;
    }

    if (FILE_SUFFIX_AUDIO.includes(fileSuffix)) {
      item.prefix = () => <FileAudio size={18} />;
    }

    if (FILE_SUFFIX_DOCUMENT.includes(fileSuffix)) {
      item.prefix = () => <FileText size={18} />;
    }

    if (FILE_SUFFIX_CODE.includes(fileSuffix)) {
      item.prefix = () => <Code size={18} />;
    }

    if (FILE_SUFFIX_INSTALL.includes(fileSuffix)) {
      item.prefix = () => <Package size={18} />;
    }
  };

  /**
   * @description 生成整个页面形式的 Tree 数据
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
   * @description 生成抽屉形式的 Table 数据
   */
  const generateTreeData = (
    sftpDataMessageData: IFileManageSftpFileItem[],
    current_path: string,
    isRootNode: boolean = true
  ) => {
    // 如果是根节点，则生成完整的树结构
    if (isRootNode) {
      const data: TreeOption[] = [];

      data.push({
        is_dir: true,
        isLeaf: false,
        key: current_path,
        label: current_path,
        path: current_path,
        children: sftpDataMessageData.map(item => {
          const fullPath = `${current_path}/${item.name}`;
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

      return data;
    }
    // 如果不是根节点，只返回子节点数组
    else {
      const children = sftpDataMessageData.map(item => {
        const fullPath = `${current_path}/${item.name}`;
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

  /**
   * @description 递归查找节点
   */
  const findNodeByPath = (nodes: TreeOption[], path: string): TreeOption | null => {
    for (const node of nodes) {
      if (node.path === path) {
        return node;
      }

      if (node.children && Array.isArray(node.children)) {
        const found = findNodeByPath(node.children, path);

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
          const current_path = transferMessage.current_path;
          const sftpDataMessageData: IFileManageSftpFileItem[] = JSON.parse(transferMessage.data);

          // 初次加载
          if (treeData.length === 0) {
            treeData.push(...generateTreeData(sftpDataMessageData, current_path, true));
            break;
          }

          const targetNode = findNodeByPath(treeData, current_path);

          if (targetNode) {
            targetNode.children = generateTreeData(sftpDataMessageData, current_path, false);
          }

          break;
      }
    };

    socket.value = ws.value;
  });

  return {
    socket,
    treeData,
    handleLoad,
    renderLabel
  };
};
