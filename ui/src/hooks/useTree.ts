import { h, ref, Ref } from 'vue';
import { NIcon, TreeOption } from 'naive-ui';
import { useLogger } from '@/hooks/useLogger.ts';
import { wsIsActivated } from '@/components/Terminal/helper';
import { customTreeOption } from '@/hooks/interface';
import { Folder } from '@vicons/ionicons5';

import SvgIcon from '@/components/SvgIcon/index.vue';

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

  // 更新节点
  const updateTreeNodes = (msg: any) => {
    const nodeId = msg.id;

    const data = JSON.parse(msg.data);

    console.log('data', data);
    console.log('currentNode', currentNode);

    const childNodes = data.map((name: string) => {
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

      // 根据当前节点的层级设置子节点的 namespace 和 pod
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
      }

      return childNode;
    });

    console.log('childNodes', childNodes);

    if (!currentNode.value.children) {
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
