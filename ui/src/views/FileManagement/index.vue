<template>
  <n-flex vertical class="w-screen h-screen !gap-y-0">
    <n-flex
      align="center"
      justify="space-between"
      class="header w-full h-14 px-6 bg-[#202222] border-b border-b-[#EBEBEB26]"
    >
      <n-flex align="center" class="h-full !gap-x-5">
        <template v-for="item of leftActionsMenu" :key="item.label">
          <n-popover>
            <template #trigger>
              <n-button text :disabled="item.disabled" @click="item.click" size="small" class="group">
                <template #icon>
                  <component
                    :is="item.icon"
                    size="18"
                    class="text-white cursor-pointer group-disabled:cursor-not-allowed focus:outline-none group-hover:text-[#16987D] group-disabled:group-hover:text-white/50 transition-colors duration-300 ease-in-out group-disabled:transition-none"
                  />
                </template>
              </n-button>
            </template>

            {{ item.label }}
          </n-popover>
        </template>
      </n-flex>
      <n-flex align="center" class="h-full w-69 !flex-nowrap">
        <n-input size="small" v-model:value="searchKeywords" :placeholder="searchPlaceholder" class="w-52">
          <template #prefix>
            <Search :size="16" />
          </template>
        </n-input>

        <n-switch size="large" v-model:value="isGrid" :round="false">
          <template #checked-icon>
            <LayoutGrid :size="16" color="#1AB394" />
          </template>
          <template #unchecked-icon>
            <List :size="16" color="#1AB394" />
          </template>
        </n-switch>
      </n-flex>
    </n-flex>

    <n-flex class="h-full w-60"> </n-flex>
    <n-split
      direction="horizontal"
      class="h-full"
      v-model:size="splitValue"
      :max="0.5"
      :min="0.2"
      :default-size="0.2"
      :resize-trigger-size="1"
    >
      <template #1>
        <n-flex vertical align="center" justify="center" class="h-full px-4 py-2 bg-[#202222]">
          <n-spin :show="loading">
            <n-tree
              block-line
              expand-on-click
              :data="treeData"
              :on-load="handleLoad"
              :node-props="nodeProps"
              :render-label="customRenderLabel"
              class="w-full h-full"
            />
          </n-spin>
        </n-flex>
      </template>
      <template #2> Pane 2 </template>
    </n-split>
  </n-flex>

  <n-modal
    preset="dialog"
    positive-text="确认"
    :show-icon="false"
    :mask-closable="false"
    v-model:show="showModal"
    @positive-click="onPositiveClick"
  >
    <template #header>
      <div class="text-lg font-bold">{{ modalTitle }}</div>
    </template>

    <template #default>
      <n-input v-if="modalType === 'create'" v-model:value="modalContent" :status="inputStatus" class="mt-4" />

      <n-tag :bordered="false" type="error" size="small">
        {{ deleteModalContent }}
      </n-tag>
      <n-text v-if="modalType === 'delete'" class="mt-4"></n-text>
    </template>
  </n-modal>
</template>

<script setup lang="ts">
import { v4 as uuid } from 'uuid';
import { useI18n } from 'vue-i18n';
import { useRoute } from 'vue-router';
import { WINDOW_MESSAGE_TYPE } from '@/enum';
import { useFileList } from '@/hooks/useFileList';
import { SFTP_CMD, FILE_MANAGE_MESSAGE_TYPE } from '@/enum';
import { sendEventToLuna } from '@/components/TerminalComponent/helper';
import { FunctionalComponent, reactive, ref, onMounted, onUnmounted, computed, watch } from 'vue';

import type { TreeOption } from 'naive-ui';

import {
  List,
  Copy,
  Trash2,
  Search,
  PenLine,
  FilePlus2,
  ArrowLeft,
  ArrowRight,
  RefreshCcw,
  FolderPlus,
  LayoutGrid,
  ClipboardCopy
} from 'lucide-vue-next';

interface LeftActionsMenu {
  label: string;

  icon: FunctionalComponent;

  disabled: boolean;

  click: () => void;
}

const { t } = useI18n();
const route = useRoute();

const modalType = ref<'create' | 'rename' | 'delete'>('create');

const isGrid = ref(false);
const loading = ref(false);
const showModal = ref(false);
const splitValue = ref(0.2);
const modalTitle = ref('');
const modalContent = ref('');
const connectToken = ref('');
const searchKeywords = ref('');
const currentNodePath = ref('');
const searchPlaceholder = ref(t('Search'));

if (route.query && typeof route.query.token === 'string') {
  connectToken.value = route.query.token;
}

const { socket, treeData, handleLoad, renderLabel } = useFileList(connectToken.value);

const customRenderLabel = computed(() => renderLabel(splitValue));
const inputStatus = computed((): 'error' | 'default' => {
  if (modalContent.value.length === 0) {
    return 'error';
  }

  return 'default';
});
const leftActionsMenu = computed((): LeftActionsMenu[] => {
  return [
    {
      label: '后退',
      icon: ArrowLeft,
      disabled: currentNodePath.value.length === 0,
      click: () => {
        console.log('后退');
      }
    },
    {
      label: '前进',
      icon: ArrowRight,
      disabled: currentNodePath.value.length === 0,
      click: () => {
        console.log('前进');
      }
    },
    {
      label: '刷新',
      icon: RefreshCcw,
      disabled: false,
      click: () => {
        loading.value = true;

        const sendData = {
          path: currentNodePath.value
        };

        const sendBody = {
          id: uuid(),
          cmd: SFTP_CMD.LIST,
          type: FILE_MANAGE_MESSAGE_TYPE.SFTP_DATA,
          data: JSON.stringify(sendData)
        };

        if (socket.value) {
          socket.value.send(JSON.stringify(sendBody));
        }
      }
    },
    {
      label: '新建文件夹',
      icon: FolderPlus,
      disabled: currentNodePath.value.length === 0,
      click: () => {
        showModal.value = true;
        modalType.value = 'create';
        modalTitle.value = t('NewFolder');

        // const sendData = {
        //   path: currentNodePath.value
        // };

        // const sendBody = {
        //   id: uuid(),
        //   cmd: SFTP_CMD.MKDIR,
        //   type: FILE_MANAGE_MESSAGE_TYPE.SFTP_DATA,
        //   data: JSON.stringify(sendData)
        // };

        // if (socket.value) {
        //   socket.value.send(JSON.stringify(sendBody));
        // }
      }
    },
    {
      label: '新增文件',
      icon: FilePlus2,
      disabled: false,
      click: () => {
        console.log('新增文件');
      }
    },
    {
      label: '复制',
      icon: Copy,
      disabled: currentNodePath.value.length === 0,
      click: () => {
        console.log('复制');
      }
    },
    {
      label: '粘贴',
      icon: ClipboardCopy,
      disabled: false,
      click: () => {
        console.log('粘贴');
      }
    },
    {
      label: '删除',
      icon: Trash2,
      disabled: currentNodePath.value.length === 0,
      click: () => {
        showModal.value = true;
        modalType.value = 'delete';
        modalTitle.value = 'Do you want to delete this file?';
      }
    },
    {
      label: '重命名',
      icon: PenLine,
      disabled: currentNodePath.value.length === 0,
      click: () => {
        console.log('重命名');
      }
    }
  ];
});
const deleteModalContent = computed((): string => {
  return currentNodePath.value.split('/').pop() || '';
});

watch(
  () => treeData,
  data => {
    if (data) {
      loading.value = false;
    }
  },
  {
    deep: true
  }
);

const handleCommunication = (event: MessageEvent) => {
  const windowMessage = event.data;

  switch (windowMessage.name) {
    case WINDOW_MESSAGE_TYPE.PING:
      sendEventToLuna(WINDOW_MESSAGE_TYPE.PONG, '', windowMessage.id, event.origin);
      break;
  }
};

const onPositiveClick = () => {
  showModal.value = false;
};

const nodeProps = ({ option }: { option: TreeOption }) => {
  return {
    onClick: () => {
      currentNodePath.value = option.path as string;
    }
  };
};

onMounted(() => {
  window.addEventListener('message', (event: MessageEvent) => handleCommunication(event));
});

onUnmounted(() => {
  window.removeEventListener('message', handleCommunication);
});
</script>

<style scoped lang="scss">
:deep(.n-spin-container) {
  width: 100%;
  height: 100%;

  .n-spin-content {
    width: 100%;
    height: 100%;

    .n-tree {
      .n-tree-node-switcher {
        display: flex;
        justify-content: center;
        align-items: center;
        height: 30px;
      }

      .n-empty.n-tree__empty {
        display: flex;
        align-items: center;
        justify-content: center;
        height: 100%;
      }
    }
  }
}
</style>
