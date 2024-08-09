// 引入 API
import { h, ref } from 'vue';

// 引入 Hook
import { useLogger } from '@/hooks/useLogger.ts';

// 引入类型
import type { Ref } from 'vue';
import type { TreeOption } from 'naive-ui';
import type { customTreeOption } from '@/hooks/interface';

// 组件
import { NIcon } from 'naive-ui';
import { Folder } from '@vicons/ionicons5';
import SvgIcon from '@/components/SvgIcon/index.vue';

import { wsIsActivated } from '@/components/Terminal/helper';

const { debug } = useLogger('Tree-Hook');

/**
 * @description 处理 Tree 数据相关的 Hook
 */
export const useTree = (
  socket: WebSocket,
  socketSend: (data: string | ArrayBuffer | Blob, useBuffer?: boolean) => boolean
) => {
  // 当前的所有 Tree 节点
  const treeNodes: Ref<TreeOption[]> = ref([]);
  const currentNode: Ref<customTreeOption | null> = ref(null);

  // 初始化 Tree
  const initTree = (key: string, label: string): TreeOption[] => {
    treeNodes.value.push({
      key,
      label,
      isLeaf: false
    });

    return treeNodes.value;
  };

  const beforeLoad = (treeNode: customTreeOption) => {
    return treeNode && !treeNode.children && !treeNode.isLeaf;
  };

  // 处理子节点
  const handleChildNodes = (name: string, nodeId: string, currentNode: customTreeOption) => {
    if (!currentNode) {
      debug('currentNode is undefined or null');
      return null;
    }

    const childNode: customTreeOption = {
      id: `${nodeId}-${name}`,
      key: `${nodeId}-${name}`,
      label: name,
      isLeaf: false,
      prefix: () =>
        h(NIcon, null, {
          default: () => h(Folder)
        })
    };

    if (!currentNode.namespace && !currentNode.pod) {
      childNode.namespace = name;
    } else if (currentNode.namespace && !currentNode.pod) {
      childNode.namespace = currentNode.namespace;
      childNode.pod = name;
    } else if (currentNode.namespace && currentNode.pod && !currentNode.container) {
      childNode.namespace = currentNode.namespace;
      childNode.pod = currentNode.pod;
      childNode.container = name;
      childNode.isLeaf = true;
      childNode.prefix = () =>
        h(SvgIcon, {
          name: 'k8s',
          iconStyle: {
            width: '14px',
            height: '14px',
            fill: '#D8D8D8'
          }
        });
    }

    return childNode;
  };

  // 更新节点
  const updateTreeNodes = (msg: any) => {
    try {
      const nodeId = msg.id;
      const data = JSON.parse(msg.data);

      if (!currentNode.value) {
        debug('currentNode is undefined or null');
        return [];
      }

      const childNodes = data.map((name: string) => {
        return handleChildNodes(name, nodeId, currentNode.value!);
      });

      if (!currentNode.value.children && !currentNode.value.isLeaf) {
        currentNode.value.children = childNodes;
      }

      return childNodes;
    } catch (error) {
      debug('Error parsing message or updating tree nodes:', error);
      return [];
    }
  };

  // 点击节点触发异步 Load 或者直接连接终端
  const syncLoadNode = (treeNode: customTreeOption, terminalId: string) => {
    if (!treeNode) return;

    currentNode.value = treeNode;

    if (!beforeLoad(treeNode)) return;

    debug('Start Load Tree Node ....');

    const loadDate: customTreeOption = {
      id: terminalId,
      pod: treeNode.pod || '',
      type: 'TERMINAL_K8S_TREE',
      k8s_id: treeNode.id,
      namespace: treeNode.namespace || ''
    };

    if (wsIsActivated(socket)) {
      socketSend(JSON.stringify(loadDate));
    }
  };

  return {
    initTree,
    syncLoadNode,
    updateTreeNodes
  };
};
