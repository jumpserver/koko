<script setup lang="ts">
import type { Ref } from 'vue';
import type { DropdownOption, TabPaneProps } from 'naive-ui';
import type { UseDraggableReturn } from 'vue-draggable-plus';

import { v4 as uuid } from 'uuid';
import { useI18n } from 'vue-i18n';
import { storeToRefs } from 'pinia';
import { useDebounceFn } from '@vueuse/core';
import { useDraggable } from 'vue-draggable-plus';
import { BrushCleaning, CircleX, Copy, RotateCcw } from 'lucide-vue-next';
import { computed, h, nextTick, onBeforeUnmount, onMounted, ref } from 'vue';

import mittBus from '@/utils/mittBus';
import { updateIcon } from '@/hooks/helper';
import { useColor } from '@/hooks/useColor';
import Drawer from '@/components/Drawer/index.vue';
import { useTreeStore } from '@/store/modules/tree.ts';
import { createTerminal } from '@/hooks/useKubernetes.ts';
import { useTerminalStore } from '@/store/modules/terminal.ts';
import TerminalProvider from '@/components/TerminalProvider/index.vue';

const treeStore = useTreeStore();
const terminalStore = useTerminalStore();

const { t } = useI18n();
const { lighten } = useColor();
const { connectInfo } = storeToRefs(treeStore);

const themeColors = computed(() => {
  const colors = {
    '--tab-bg-color': lighten(3),
    '--tab-inactive-bg-color': lighten(6),
    '--tab-active-bg-color': lighten(10),
    '--tab-inactive-text-color': lighten(50),
    '--tab-active-text-color': lighten(60),
    '--icon-color': lighten(45),
    '--icon-hover-bg-color': lighten(8),
  };

  return colors;
});

const nameRef = ref('');
const showDrawer = ref<boolean>(false);
const contextIdentification = ref('');
const dropdownY = ref(0);
const dropdownX = ref(0);
const showContextMenu = ref(false);
const panels: Ref<TabPaneProps[]> = ref([]);

const processedElements = new Set();
const contextMenuOption = [
  {
    label: t('Reconnect'),
    key: 'reconnect',
    icon: () => h(RotateCcw, { size: 16 }),
  },
  {
    label: t('Close Current Tab'),
    key: 'close',
    icon: () => h(CircleX, { size: 16 }),
  },
  {
    label: t('Close All Tabs'),
    key: 'closeAll',
    icon: () => h(BrushCleaning, { size: 16 }),
  },
  {
    label: t('Clone Connect'),
    key: 'cloneConnect',
    icon: () => h(Copy, { size: 16 }),
  },
];

const swapElements = (arr: any[], index1: number, index2: number) => {
  [arr[index1], arr[index2]] = [arr[index2], arr[index1]];
  return arr;
};

const findNodeById = (nameRef: string) => {
  const treeStore = useTreeStore();

  for (const [_key, value] of treeStore.terminalMap.entries()) {
    if (value.k8s_id === nameRef) {
      treeStore.setCurrentNode(value);

      // 恢复当前节点的 ctrlCAsCtrlZ 配置
      if (value.ctrlCAsCtrlZMap && value.ctrlCAsCtrlZMap.has(value.k8s_id)) {
        const ctrlCAsCtrl: string = value.ctrlCAsCtrlZMap.get(value.k8s_id);
        terminalStore.setTerminalConfig('ctrlCAsCtrlZ', ctrlCAsCtrl);
      }

      // 恢复当前节点的 backspaceAsCtrlH 配置
      if (value.backspaceAsCtrlHMap && value.backspaceAsCtrlHMap.has(value.k8s_id)) {
        const backspaceAsCtrlH: string = value.backspaceAsCtrlHMap.get(value.k8s_id);
        terminalStore.setTerminalConfig('backspaceAsCtrlH', backspaceAsCtrlH);
      }
    }
  }
};

/**
 * @description 处理标签关闭
 *
 * @param name
 */
function handleClose(name: string) {
  const node = treeStore.getTerminalByK8sId(name);
  const socket = node.socket;

  if (socket) {
    socket.send(
      JSON.stringify({
        type: 'K8S_CLOSE',
        id: node.id,
        k8s_id: node.k8s_id,
      })
    );
  }

  const index = panels.value.findIndex(panel => panel.name === name);

  panels.value.splice(index, 1);

  treeStore.removeK8sIdMap(name);

  const panelLength = panels.value.length;

  // 只有当 tab 的数量大于 1 并且为当前所在的 tab 在关闭时才会自动定位到前一位
  if (panelLength >= 1 && nameRef.value === name) {
    nameRef.value = panels.value[panelLength - 1].name as string;
    findNodeById(nameRef.value);
    terminalStore.setTerminalConfig('currentTab', nameRef.value);
  }
}

/**
 * @description 切换标签
 *
 * @param value
 */
function handleChangeTab(value: string) {
  nameRef.value = value;

  findNodeById(value);

  terminalStore.setTerminalConfig('currentTab', value);
}

/**
 * @description 每个 tab 标签的右侧快捷功能
 * @param e
 */
function handleContextMenu(e: PointerEvent) {
  let target: HTMLElement = e.target as HTMLElement;

  while (target && !target.hasAttribute('data-name')) {
    target = target.parentElement as HTMLElement;
  }

  if (target) {
    // 获取设置的 data 属性
    const dataName: string = target.getAttribute('data-name') as string;

    if (dataName) {
      contextIdentification.value = dataName;

      e.preventDefault();
      showContextMenu.value = true;
      dropdownY.value = e.clientY;
      dropdownX.value = e.clientX;
    }
  }
}

/**
 * @description 重新连接
 */
function handleReconnect(type: string) {
  const operatedNode = treeStore.getTerminalByK8sId(contextIdentification.value);
  const socket = operatedNode?.socket;

  if (type === 'reconnect') {
    if (socket) {
      socket.send(
        JSON.stringify({
          type: 'K8S_CLOSE',
          id: operatedNode.id,
          k8s_id: operatedNode.k8s_id,
        })
      );
    }

    // 找到所操作节点的下标，
    const index = panels.value.findIndex(panel => panel.name === contextIdentification.value);

    panels.value.splice(index, 1);
    treeStore.removeK8sIdMap(operatedNode.k8s_id);

    const newId = uuid();

    operatedNode.key = newId;
    operatedNode.k8s_id = newId;
    operatedNode.position = index;

    mittBus.emit('connect-terminal', { ...operatedNode });
  } else if (type === 'cloneConnect') {
    mittBus.emit('connect-terminal', { ...operatedNode });
  }

  showContextMenu.value = false;
}

/**
 * @description 右键菜单的回调
 *
 * @param key
 * @param _option
 */
function handleContextMenuSelect(key: string, _option: DropdownOption) {
  switch (key) {
    case 'reconnect': {
      // 对于重新连接来说只有 k8sid 需要变化，并且需要发送 K8S_CLOSE 时间
      handleReconnect('reconnect');
      break;
    }
    case 'close': {
      handleClose(contextIdentification.value);
      showContextMenu.value = false;
      break;
    }
    case 'closeAll': {
      panels.value.forEach((panel: any) => {
        treeStore.removeK8sIdMap(panel.k8s_id);
      });

      panels.value = [];

      showContextMenu.value = false;
      break;
    }
    case 'cloneConnect': {
      handleReconnect('cloneConnect');

      break;
    }
  }
}

/**
 * @description 更新 tab 的唯一标识
 *
 * @param key
 */
function updateTabElements(key: string) {
  const tabElements = document.querySelectorAll('.n-tabs-tab-wrapper');

  tabElements.forEach(element => {
    if (!processedElements.has(element)) {
      element.setAttribute('data-identification', key);
      processedElements.add(element);
    }
  });
}

/**
 * @description 关闭右侧菜单
 */
function handleClickOutside() {
  showContextMenu.value = false;
}

/**
 * @description tab item 的拖拽处理
 */
function initializeDraggable() {
  const tabsContainer = document.querySelector('.n-tabs-wrapper');

  if (tabsContainer) {
    // 对于 useDraggable 如果直接操作 panel 可能会导致被注入一个 undefined 值从而导致报错，因此下面代码全部使用副本来操作
    useDraggable<UseDraggableReturn>(
      // @ts-expect-error 类型错误
      tabsContainer,
      JSON.parse(JSON.stringify(panels.value)),
      {
        animation: 150,
        onEnd: async event => {
          if (!event || event.newIndex === undefined || event.oldIndex === undefined) {
            return console.warn('Event or index is undefined');
          }

          const newIndex = event!.newIndex - 1;
          const oldIndex = event!.oldIndex - 1;

          // 此处不能使用 JSON.parse(JSON.stringify) 的形式，否则会出现循环引用, 只需浅拷贝即可
          const clonedPanels = panels.value.map(panel => ({ ...panel }));

          panels.value = swapElements(clonedPanels, newIndex, oldIndex).filter(panel => panel !== null);

          const newActiveTab: string = panels.value[newIndex!]?.name as string;

          if (newActiveTab) {
            nameRef.value = newActiveTab;
            findNodeById(newActiveTab);
            terminalStore.setTerminalConfig('currentTab', newActiveTab);
          }
        },
      }
    );
  }
}

/**
 * @description 切换到上一个 Tab
 */
function switchToPreviousTab() {
  const currentIndex = panels.value.findIndex(panel => panel.name === nameRef.value);

  if (currentIndex > 0) {
    nameRef.value = panels.value[currentIndex - 1].name as string;
  } else {
    nameRef.value = panels.value[panels.value.length - 1].name as string;
  }

  findNodeById(nameRef.value);

  terminalStore.setTerminalConfig('currentTab', nameRef.value);
}

/**
 * @description 切换到下一个 Tab
 */
function switchToNextTab() {
  const currentIndex = panels.value.findIndex(panel => panel.name === nameRef.value);

  if (currentIndex < panels.value.length - 1) {
    nameRef.value = panels.value[currentIndex + 1].name as string;
  } else {
    nameRef.value = panels.value[0].name as string;
  }

  findNodeById(nameRef.value);

  terminalStore.setTerminalConfig('currentTab', nameRef.value);
}

const debouncedSwitchToPreviousTab = useDebounceFn(() => {
  switchToPreviousTab();
}, 200);

const debouncedSwitchToNextTab = useDebounceFn(() => {
  switchToNextTab();
}, 200);

function unloadEvent() {
  mittBus.off('sync-theme');
  mittBus.off('share-user');
  mittBus.off('terminal-search');
  mittBus.off('create-share-url');
  mittBus.off('remove-share-user');
}

onMounted(() => {
  const lunaConfig = terminalStore.getConfig;

  nextTick(() => {
    initializeDraggable();
  });

  mittBus.on('open-setting', () => {
    showDrawer.value = true;
  });

  mittBus.on('connect-terminal', (node: any) => {
    let index;

    // 如果在 panels 中有相同的 k8s_id，则认为是对一个节点重复连接
    panels.value.forEach(panel => {
      if (panel.name === node.k8s_id) {
        const newId = uuid();
        node.key = newId;
        node.k8s_id = newId;
      }
    });

    if (node.position || node.position === 0) {
      index = node.position;
    } else {
      index = panels.value.length;
    }

    panels.value.splice(index, 0, {
      ...node,
      // 二者为组件库的必填项
      name: node.k8s_id,
      tab: node.label,
    });

    nameRef.value = node.k8s_id;

    nextTick(() => {
      treeStore.setCurrentNode(node);
      terminalStore.setTerminalConfig('currentTab', node.k8s_id);

      unloadEvent();
      updateTabElements(node.k8s_id);

      const el = document.getElementById(node.k8s_id);

      if (el) {
        const terminal = createTerminal(el, node.socket, lunaConfig, node);

        treeStore.setK8sIdMap(node.k8s_id, {
          ...node,
          terminal,
        });

        const firstSendMessage = {
          id: node.id,
          k8s_id: node.k8s_id,
          namespace: node.namespace || '',
          pod: node.pod || '',
          container: node.container || '',
          type: 'TERMINAL_K8S_INIT',
          data: JSON.stringify({
            cols: terminal.cols,
            rows: terminal.rows,
            code: '',
          }),
        };

        try {
          // 发送初次连接的数据
          node.socket.send(JSON.stringify(firstSendMessage));

          updateIcon(connectInfo.value);
        } catch (e: any) {
          throw new Error(e);
        }
      }
    });
  });

  mittBus.on('alt-shift-left', debouncedSwitchToPreviousTab);
  mittBus.on('alt-shift-right', debouncedSwitchToNextTab);
});

onBeforeUnmount(() => {
  mittBus.off('alt-shift-left', debouncedSwitchToPreviousTab);
  mittBus.off('alt-shift-right', debouncedSwitchToNextTab);
  mittBus.off('connect-terminal');
});
</script>

<template>
  <TerminalProvider>
    <template #terminal>
      <n-layout :native-scrollbar="false" content-style="height: 100%" :style="themeColors">
        <n-tabs
          v-model:value="nameRef"
          closable
          size="small"
          type="card"
          tab="show:lazy"
          tab-style="min-width: 80px;"
          class="header-tab relative"
          @close="handleClose"
          @update:value="handleChangeTab"
          @contextmenu.prevent="handleContextMenu"
        >
          <n-tab-pane
            v-for="panel of panels"
            :key="panel.name"
            :tab="panel.tab"
            :name="panel.name"
            display-directive="show:lazy"
            class="pt-0"
          >
            <n-layout :native-scrollbar="false">
              <n-scrollbar trigger="hover">
                <div :id="String(panel.name)" class="k8s-terminal" />
              </n-scrollbar>
            </n-layout>
          </n-tab-pane>
        </n-tabs>
      </n-layout>
      <n-dropdown
        show-arrow
        size="medium"
        trigger="manual"
        placement="bottom-start"
        content-style='font-size: "13px"'
        :x="dropdownX"
        :y="dropdownY"
        :show="showContextMenu"
        :options="contextMenuOption"
        @select="handleContextMenuSelect"
        @clickoutside="handleClickOutside"
      />
    </template>

    <template #drawer>
      <Drawer :hidden-file-manager="true" />
    </template>
  </TerminalProvider>
</template>

<style scoped lang="scss">
@use './index.scss';
</style>
