import { h } from 'vue';
import { NIcon, NPopover } from 'naive-ui';

import type { Component } from 'vue';
import type { TreeOption } from 'naive-ui';
import type { customTreeOption } from '@/hooks/interface';
import { useTreeStore } from '@/store/modules/tree.ts';
// import { useTerminalStore } from '@/store/modules/terminal.ts';

/**
 * @description 用于每个 Tree Item 的 Tooltip
 * @param option
 */
export const showToolTip = (option: TreeOption) => {
  const customOption = option.option as customTreeOption;

  return h(
    NPopover,
    {
      trigger: 'hover',
      placement: 'top-start',
      delay: 1000
    },
    {
      trigger: () =>
        h(
          'span',
          { style: { display: 'inline-block', whiteSpace: 'nowrap' } },
          customOption.label
        ),
      default: () => customOption.label
    }
  );
};

/**
 * @description 用于渲染 setting 中的图标
 */
export const renderIcon = (icon: Component) => {
  return () => {
    return h(NIcon, null, {
      default: () => h(icon)
    });
  };
};

/**
 * 交换数组中的某两个值
 *
 * @param arr
 * @param index1
 * @param index2
 */
export const swapElements = (arr: any[], index1: number, index2: number) => {
  [arr[index1], arr[index2]] = [arr[index2], arr[index1]];
  return arr;
};

/**
 * 根据 id 查找对应的节点信息
 */
export const findNodeById = (nameRef: string) => {
  const treeStore = useTreeStore();
  // const terminalStore = useTerminalStore();

  for (const [_key, value] of treeStore.terminalMap.entries()) {
    if (value.k8s_id === nameRef) {
      treeStore.setCurrentNode(value);
      // const ctrlCAsCtrl: string = value.ctrlCAsCtrlZMap.get(value.k8s_id);
      //
      // terminalStore.setTerminalConfig('ctrlCAsCtrlZ', ctrlCAsCtrl);
    }
  }
};
