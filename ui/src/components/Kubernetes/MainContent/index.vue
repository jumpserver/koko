<template>
  <n-layout :native-scrollbar="false" content-style="height: 100%">
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
        class="bg-[#101014] pt-0"
      >
        <n-layout :native-scrollbar="false">
          <n-scrollbar trigger="hover">
            <div :id="String(panel.name)" class="k8s-terminal"></div>
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
  <Settings :settings="settings" />
</template>

<script setup lang="ts">
import xtermTheme from 'xterm-theme';
import mittBus from '@/utils/mittBus';
import Share from '@/components/Share/index.vue';
import Settings from '@/components/Settings/index.vue';
import ThemeConfig from '@/components/ThemeConfig/index.vue';

import {
  computed,
  h,
  markRaw,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref
} from 'vue';
import {
  findNodeById,
  renderIcon,
  swapElements
} from '@/components/Kubernetes/helper';
import { updateIcon } from '@/hooks/helper';
import { useTerminalStore } from '@/store/modules/terminal.ts';
import { useParamsStore } from '@/store/modules/params.ts';
import { createTerminal } from '@/hooks/useKubernetes.ts';
import { useTreeStore } from '@/store/modules/tree.ts';
import { useDraggable } from 'vue-draggable-plus';
import { readText } from 'clipboard-polyfill';
import { useDebounceFn } from '@vueuse/core';
import { storeToRefs } from 'pinia';
import { useI18n } from 'vue-i18n';
import { NMessageProvider, useDialog, useMessage } from 'naive-ui';
import { v4 as uuid } from 'uuid';

import type { UseDraggableReturn } from 'vue-draggable-plus';
import type { DropdownOption, TabPaneProps } from 'naive-ui';
import type { ISettingProp } from '@/types';
import type { Ref } from 'vue';

import {
  ArrowBack,
  ArrowDown,
  ArrowForward,
  ArrowUp,
  CloseCircleOutline
} from '@vicons/ionicons5';
import { ClosedCaption32Regular } from '@vicons/fluent';
import { RefreshFilled } from '@vicons/material';
import { CloneRegular } from '@vicons/fa';
import { defaultTheme } from '@/utils/config';
import {
  Activity,
  ColorPalette,
  Keyboard,
  NotSent,
  Paste,
  Share as ShareIcon,
  Stop,
  UserAvatar
} from '@vicons/carbon';


const dialog = useDialog();
const message = useMessage();
const treeStore = useTreeStore();
const paramsStore = useParamsStore();
const terminalStore = useTerminalStore();

const { t } = useI18n();
const { connectInfo } = storeToRefs(treeStore);

const nameRef = ref('');
const showDrawer = ref<boolean>(false);
const contextIdentification = ref('');
const themeName = ref('Default');
const dropdownY = ref(0);
const dropdownX = ref(0);
const showContextMenu = ref(false);
const panels: Ref<TabPaneProps[]> = ref([]);

const processedElements = new Set();
const contextMenuOption = [
  {
    label: t('Reconnect'),
    key: 'reconnect',
    icon: renderIcon(RefreshFilled)
  },
  {
    label: t('Close Current Tab'),
    key: 'close',
    icon: renderIcon(CloseCircleOutline)
  },
  {
    label: t('Close All Tabs'),
    key: 'closeAll',
    icon: renderIcon(ClosedCaption32Regular)
  },
  {
    label: t('Clone Connect'),
    key: 'cloneConnect',
    icon: renderIcon(CloneRegular)
  }
];

const settings = computed((): ISettingProp[] => {
  return [
    {
      label: 'ThemeConfig',
      title: t('ThemeConfig'),
      icon: ColorPalette,
      disabled: () => {
        const operatedNode = treeStore.getTerminalByK8sId(nameRef.value);

        return !(operatedNode && operatedNode.terminal);
      },
      click: () => {
        dialog.success({
          class: 'set-theme',
          title: t('Theme'),
          showIcon: false,
          style: 'width: 50%; min-width: 810px',
          content: () => {
            const operatedNode = treeStore.getTerminalByK8sId(nameRef.value);

            return h(ThemeConfig, {
              currentThemeName: themeName.value,
              preview: (tempTheme: string) => {
                operatedNode.terminal.options.theme =
                  xtermTheme[tempTheme] || defaultTheme;
              }
            });
          }
        });
        // 关闭抽屉
        mittBus.emit('open-setting');
      }
    },
    {
      label: 'Share',
      title: t('Share'),
      icon: ShareIcon,
      disabled: () => {
        const operatedNode = treeStore.getTerminalByK8sId(nameRef.value);

        return !operatedNode?.enableShare;
      },
      click: () => {
        const operatedNode = treeStore.getTerminalByK8sId(nameRef.value);
        const sessionId = operatedNode.sessionIdMap.get(operatedNode.k8s_id);

        dialog.success({
          class: 'share',
          title: t('CreateLink'),
          showIcon: false,
          style: 'width: 35%; min-width: 500px',
          content: () => {
            return h(NMessageProvider, null, {
              default: () =>
                h(Share, {
                  sessionId,
                  enableShare: operatedNode?.enableShare,
                  userOptions: operatedNode?.userOptions
                })
            });
          },
          onClose: () => resetShareDialog(),
          onMaskClick: () => resetShareDialog()
        });
        // 关闭抽屉
        mittBus.emit('open-setting');
      }
    },
    {
      label: 'User',
      title: t('User'),
      icon: UserAvatar,
      disabled: () => {
        const operatedNode = treeStore.getTerminalByK8sId(nameRef.value);
        return Object.keys(operatedNode.onlineUsersMap).length < 1;
      },
      content: () => {
        const operatedNode = treeStore.getTerminalByK8sId(nameRef.value);

        if (operatedNode && operatedNode.onlineUsersMap) {
          return Object.entries(operatedNode?.onlineUsersMap)
            .flatMap(([sessionKey, items]) =>
              // @ts-ignore
              items
                .filter((_item: any) => {
                  const operatedNode = treeStore.getTerminalByK8sId(
                    nameRef.value
                  );
                  return operatedNode.k8s_id === sessionKey;
                })
                .map((item: any) => {
                  return {
                    ...item,
                    name: item.user,
                    icon: item.writable ? markRaw(Activity) : markRaw(NotSent),
                    tip: item.writable ? t('Writable') : t('ReadOnly'),
                    sessionKey // 添加会话的 key 值
                  };
                })
            )
            .sort(
              (a, b) =>
                new Date(a.created).getTime() - new Date(b.created).getTime()
            );
        }

        return [];
      },
      click: user => {
        if (user.primary) return;

        dialog.warning({
          title: t('Warning'),
          content: t('RemoveShareUserConfirm'),
          positiveText: t('ConfirmBtn'),
          negativeText: t('Cancel'),
          onPositiveClick: () => {
            const operatedNode = treeStore.getTerminalByK8sId(nameRef.value);
            const sessionId = operatedNode.sessionIdMap.get(
              operatedNode.k8s_id
            );

            mittBus.emit('remove-share-user', {
              sessionId: sessionId,
              userMeta: user,
              type: 'TERMINAL_SHARE_USER_REMOVE'
            });
          }
        });
      }
    },
    {
      label: 'Keyboard',
      title: t('Hotkeys'),
      icon: Keyboard,
      content: [
        {
          name: 'Ctrl + C',
          icon: Stop,
          tip: t('Cancel'),
          click: () => {
            handleWriteData('Stop');
          }
        },
        {
          name: 'Command/Ctrl + V',
          icon: Paste,
          tip: t('Paste'),
          click: () => {
            handleWriteData('Paste');
          }
        },
        {
          name: 'Arrow Up',
          icon: ArrowUp,
          tip: t('UpArrow'),
          click: () => {
            handleWriteData('ArrowUp');
          }
        },
        {
          name: 'Arrow Down',
          icon: ArrowDown,
          tip: t('DownArrow'),
          click: () => {
            handleWriteData('ArrowDown');
          }
        },
        {
          name: 'Arrow Left',
          icon: ArrowBack,
          tip: t('LeftArrow'),
          click: () => {
            handleWriteData('ArrowLeft');
          }
        },
        {
          name: 'Arrow Right',
          icon: ArrowForward,
          tip: t('RightArrow'),
          click: () => {
            handleWriteData('ArrowRight');
          }
        }
      ],
      disabled: () => false,
      click: () => {}
    }
  ];
});

/**
 * @description 处理标签关闭
 *
 * @param name
 */
const handleClose = (name: string) => {
  const node = treeStore.getTerminalByK8sId(name);
  const socket = node.socket;

  if (socket) {
    socket.send(
      JSON.stringify({
        type: 'K8S_CLOSE',
        id: node.id,
        k8s_id: node.k8s_id
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
};

/**
 * @description 切换标签
 *
 * @param value
 */
const handleChangeTab = (value: string) => {
  nameRef.value = value;

  findNodeById(value);

  terminalStore.setTerminalConfig('currentTab', value);
};

/**
 * @description 每个 tab 标签的右侧快捷功能
 * @param e
 */
const handleContextMenu = (e: PointerEvent) => {
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
};

/**
 * @description 重新连接
 */
const handleReconnect = (type: string) => {
  const operatedNode = treeStore.getTerminalByK8sId(
    contextIdentification.value
  );
  const socket = operatedNode?.socket;

  if (type === 'reconnect') {
    if (socket) {
      socket.send(
        JSON.stringify({
          type: 'K8S_CLOSE',
          id: operatedNode.id,
          k8s_id: operatedNode.k8s_id
        })
      );
    }

    // 找到所操作节点的下标，
    const index = panels.value.findIndex(
      panel => panel.name === contextIdentification.value
    );

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
};

/**
 * @description 右键菜单的回调
 *
 * @param key
 * @param _option
 */
const handleContextMenuSelect = (key: string, _option: DropdownOption) => {
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
};

/**
 * @description 更新 tab 的唯一标识
 *
 * @param key
 */
const updateTabElements = (key: string) => {
  const tabElements = document.querySelectorAll('.n-tabs-tab-wrapper');

  tabElements.forEach(element => {
    if (!processedElements.has(element)) {
      element.setAttribute('data-identification', key);
      processedElements.add(element);
    }
  });
};

/**
 * @description 关闭右侧菜单
 */
const handleClickOutside = () => {
  showContextMenu.value = false;
};

/**
 * @description tab item 的拖拽处理
 */
const initializeDraggable = () => {
  const tabsContainer = document.querySelector('.n-tabs-wrapper');

  if (tabsContainer) {
    // 对于 useDraggable 如果直接操作 panel 可能会导致被注入一个 undefined 值从而导致报错，因此下面代码全部使用副本来操作
    // @ts-ignore
    useDraggable<UseDraggableReturn>(
      // @ts-ignore
      tabsContainer,
      JSON.parse(JSON.stringify(panels.value)),
      {
        animation: 150,
        onEnd: async event => {
          if (
            !event ||
            event.newIndex === undefined ||
            event.oldIndex === undefined
          ) {
            return console.warn('Event or index is undefined');
          }

          let newIndex = event!.newIndex - 1;
          let oldIndex = event!.oldIndex - 1;

          // 此处不能使用 JSON.parse(JSON.stringify) 的形式，否则会出现循环引用, 只需浅拷贝即可
          const clonedPanels = panels.value.map(panel => ({ ...panel }));

          panels.value = swapElements(clonedPanels, newIndex, oldIndex).filter(
            panel => panel !== null
          );

          const newActiveTab: string = panels.value[newIndex!]?.name as string;

          if (newActiveTab) {
            nameRef.value = newActiveTab;
            findNodeById(newActiveTab);
            terminalStore.setTerminalConfig('currentTab', newActiveTab);
          }
        }
      }
    );
  }
};

/**
 * @description 重置分享表单的数据
 */
const resetShareDialog = () => {
  const operatedNode = treeStore.getTerminalByK8sId(nameRef.value);
  operatedNode.userOptions = [];

  paramsStore.setShareId('');
  paramsStore.setShareCode('');

  treeStore.setK8sIdMap(nameRef.value, { ...operatedNode });
  dialog.destroyAll();
};

/**
 * @description 向终端写入快捷命令
 *
 * @param type
 */
const handleWriteData = async (type: string) => {
  const operatedNode = treeStore.getTerminalByK8sId(nameRef.value);
  const terminal = operatedNode.terminal;

  if (!terminal) {
    return message.error(t('No terminal instances available'));
  }

  switch (type) {
    case 'Paste': {
      terminal.paste(await readText());
      break;
    }
    case 'Stop': {
      terminal.paste('\x03');
      break;
    }
    case 'ArrowUp': {
      terminal.paste('\x1b[A');
      break;
    }
    case 'ArrowDown': {
      terminal.paste('\x1b[B');
      break;
    }
    case 'ArrowLeft': {
      terminal.paste('\x1b[D');
      break;
    }
    case 'ArrowRight': {
      terminal.paste('\x1b[C');
      break;
    }
  }

  await nextTick(() => {
    terminal.focus();
  });
};

/**
 * @description 切换到上一个 Tab
 */
const switchToPreviousTab = () => {
  const currentIndex = panels.value.findIndex(
    panel => panel.name === nameRef.value
  );

  if (currentIndex > 0) {
    nameRef.value = panels.value[currentIndex - 1].name as string;
  } else {
    nameRef.value = panels.value[panels.value.length - 1].name as string;
  }

  findNodeById(nameRef.value);

  terminalStore.setTerminalConfig('currentTab', nameRef.value);
};

/**
 * @description 切换到下一个 Tab
 */
const switchToNextTab = () => {
  const currentIndex = panels.value.findIndex(
    panel => panel.name === nameRef.value
  );

  if (currentIndex < panels.value.length - 1) {
    nameRef.value = panels.value[currentIndex + 1].name as string;
  } else {
    nameRef.value = panels.value[0].name as string;
  }

  findNodeById(nameRef.value);

  terminalStore.setTerminalConfig('currentTab', nameRef.value);
};

const debouncedSwitchToPreviousTab = useDebounceFn(() => {
  switchToPreviousTab();
}, 200);

const debouncedSwitchToNextTab = useDebounceFn(() => {
  switchToNextTab();
}, 200);

const unloadEvent = () => {
  mittBus.off('sync-theme');
  mittBus.off('share-user');
  mittBus.off('terminal-search');
  mittBus.off('create-share-url');
  mittBus.off('remove-share-user');
};

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
      tab: node.label
    });

    nameRef.value = node.k8s_id;

    nextTick(() => {
      treeStore.setCurrentNode(node);
      terminalStore.setTerminalConfig('currentTab', node.k8s_id);

      unloadEvent();
      updateTabElements(node.k8s_id);

      const el = document.getElementById(node.k8s_id);

      if (el) {
        const terminal = createTerminal(el, node.socket, lunaConfig);

        treeStore.setK8sIdMap(node.k8s_id, {
          ...node,
          terminal
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
            code: ''
          })
        };

        try {
          // 发送初次连接的数据
          node.socket.send(JSON.stringify(firstSendMessage));

          const currentNode = treeStore.getTerminalByK8sId(node.k8s_id);

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

<style scoped lang="scss">
@use './index.scss';
</style>
