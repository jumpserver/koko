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
import mittBus from '@/utils/mittBus.ts';
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
  const currentNode: Ref<customTreeOption> = ref({});

  // 初始化 Tree
  const initTree = (key: string, label: string): TreeOption[] => {
    treeNodes.value.push({
      key,
      label,
      isLeaf: false
    });

    return treeNodes.value;
  };

  const beforeLoad = (treeNodes: customTreeOption) => {
    // treeNodes 不存在时表示已经加载过
    // 只有当 treeNodes 没有子节点，并且 treeNodes 存在是才会返回 true
    return treeNodes && !treeNodes.children;
  };

  // 点击节点触发异步 Load
  const syncLoadNode = (treeNodes: customTreeOption, terminalId: string) => {
    const needLoad = beforeLoad(treeNodes);

    currentNode.value = treeNodes;

    if (needLoad) {
      debug('Start Load Tree Node ....');

      const loadDate: customTreeOption = {
        id: terminalId,
        pod: treeNodes?.pod || '',
        type: 'TERMINAL_K8S_TREE',
        k8s_id: treeNodes.id,
        namespace: treeNodes?.namespace || ''
      };

      wsIsActivated(socket) && socketSend(JSON.stringify(loadDate));
    }
  };

  // 处理子节点
  const handleChildNodes = (name: string, nodeId: string) => {
    let childNode: customTreeOption = {
      id: `${nodeId}-${name}`,
      key: `${nodeId}-${name}`,
      label: name,
      isLeaf: false,
      prefix: () =>
        h(NIcon, null, {
          default: () => h(Folder)
        })
    };

    if (!currentNode.value.namespace && !currentNode.value.pod) {
      childNode.namespace = name;
    } else if (currentNode.value.namespace && !currentNode.value.pod) {
      childNode.namespace = currentNode.value.namespace;
      childNode.pod = name;
    } else if (
      currentNode.value.namespace &&
      currentNode.value.pod &&
      !currentNode.value.container
    ) {
      childNode.container = currentNode.value.container;
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
      childNode.container = name;

      childNode.key = `${currentNode.value.namespace}-${currentNode.value.pod}-${childNode.container}`;
    }

    return childNode;
  };

  // 更新节点
  const updateTreeNodes = (msg: any) => {
    const nodeId = msg.id;

    const data = JSON.parse(msg.data);

    // 如果当前节点已经有 container，直接返回空数组，不再处理子节点
    if (currentNode.value.container) {
      // 发起 k8s 的 Terminal 请求
      mittBus.emit('connect-terminal', currentNode.value);
      return [];
    }

    const childNodes = data
      .map((name: string) => {
        if (currentNode.value.container) {
          return null;
        }
        return handleChildNodes(name, nodeId);
      })
      .filter(Boolean);

    if (!currentNode.value.children && !currentNode.value.isLeaf) {
      currentNode.value.children = childNodes;
    }

    return childNodes;
  };

  return {
    initTree,
    syncLoadNode,
    updateTreeNodes
  };
};
