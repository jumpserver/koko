<template>
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
      <n-input
        v-if="modalType === 'create'"
        clearable
        class="mt-4"
        :status="inputStatus"
        v-model:value="modalContent"
        @change="onChange"
      />

      <n-tag v-if="modalType === 'delete'" :bordered="false" type="error" size="small">
        {{ deleteModalContent }}
      </n-tag>
    </template>
  </n-modal>
</template>

<script setup lang="ts">
import { v4 as uuid } from 'uuid';
import { useI18n } from 'vue-i18n';
import { computed, ref, FunctionalComponent } from 'vue';
import { SFTP_CMD, FILE_MANAGE_MESSAGE_TYPE } from '@/enum';
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

type ModalType = 'create' | 'rename' | 'delete';

const props = defineProps<{
  socket: WebSocket | null;

  currentNodePath: string;
}>();

const emits = defineEmits<{
  (e: 'update:loading', value: boolean): void;
}>()

const { t } = useI18n();

const isGrid = ref(false);
const showModal = ref(false);
const modalTitle = ref('');
const modalContent = ref('');
const searchKeywords = ref('');
const searchPlaceholder = ref(t('Search'));
const modalType = ref<ModalType>('create');

const inputStatus = computed((): 'error' | 'default' => {
  // if (modalContent.value.length === 0) {
  //   return 'error';
  // }

  return 'default';
});
const leftActionsMenu = computed((): LeftActionsMenu[] => {
  return [
    {
      label: '后退',
      icon: ArrowLeft,
      disabled: props.currentNodePath.length === 0,
      click: () => {
        console.log('后退');
      }
    },
    {
      label: '前进',
      icon: ArrowRight,
      disabled: props.currentNodePath.length === 0,
      click: () => {
        console.log('前进');
      }
    },
    {
      label: '刷新',
      icon: RefreshCcw,
      disabled: false,
      click: () => {
        emits('update:loading', true);

        const sendData = {
          path: props.currentNodePath
        };

        const sendBody = {
          id: uuid(),
          cmd: SFTP_CMD.LIST,
          type: FILE_MANAGE_MESSAGE_TYPE.SFTP_DATA,
          data: JSON.stringify(sendData)
        };

        if (props.socket) {
          props.socket.send(JSON.stringify(sendBody));
        }
      }
    },
    {
      label: '新建文件夹',
      icon: FolderPlus,
      disabled: props.currentNodePath.length === 0,
      click: () => {
        showModal.value = true;
        modalType.value = 'create';
        modalTitle.value = t('NewFolder');

        // const sendData = {
        //   path: props.currentNodePath
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
      disabled: props.currentNodePath.length === 0,
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
      disabled: props.currentNodePath.length === 0,
      click: () => {
        showModal.value = true;
        modalType.value = 'delete';
        modalTitle.value = 'Do you want to delete this file?';
      }
    },
    {
      label: '重命名',
      icon: PenLine,
      disabled: props.currentNodePath.length === 0,
      click: () => {
        console.log('重命名');
      }
    }
  ];
});
const deleteModalContent = computed((): string => {
  return props.currentNodePath.split('/').pop() || '';
});

const onPositiveClick = () => {
  showModal.value = false;
};

const onChange = (value: string) => {
  console.log(value);
};
</script>
