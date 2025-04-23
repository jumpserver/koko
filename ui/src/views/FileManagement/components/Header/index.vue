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

      <n-switch size="large" v-model:value="isGrid" :round="false" @update-value="handleLayoutChange">
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
    :positive-button-props="MODAL_BUTTON_PROPS"
    v-model:show="showModal"
    @positive-click="handleModalConfirm"
  >
    <template #header>
      <div class="text-lg font-bold">{{ modalTitle }}</div>
    </template>

    <template #default>
      <n-input
        v-if="modalType === ModalTypes.CREATE || modalType === ModalTypes.RENAME"
        clearable
        class="mt-4"
        :status="inputStatus"
        v-model:value="modalContent"
        @change="handleInputChange"
      />

      <n-tag v-if="modalType === ModalTypes.DELETE" :bordered="false" type="error" size="small">
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
  RefreshCcw,
  FolderPlus,
  LayoutGrid,
  ClipboardCopy
} from 'lucide-vue-next';

const MODAL_BUTTON_PROPS = {
  type: 'primary',
  size: 'small'
};

const ModalTypes = {
  CREATE: 'create',
  RENAME: 'rename',
  DELETE: 'delete'
} as const;

type ModalType = (typeof ModalTypes)[keyof typeof ModalTypes];

interface LeftActionsMenu {
  label: string;
  icon: FunctionalComponent;
  disabled: boolean;
  click: () => void;
}

const props = defineProps<{
  socket: WebSocket | null;

  initialPath: string;

  currentNodePath: string;
}>();

const emits = defineEmits<{
  (e: 'update:layout', value: boolean): void;
  (e: 'update:loading', value: boolean): void;
}>();

const { t } = useI18n();

const isGrid = ref(false);
const showModal = ref(false);
const modalTitle = ref('');
const modalContent = ref('');
const searchKeywords = ref('');
const searchPlaceholder = ref(t('Search'));
const modalType = ref<ModalType>(ModalTypes.CREATE);

const inputStatus = computed((): 'error' | 'default' => {
  return 'default';
});

const deleteModalContent = computed((): string => {
  return props.currentNodePath.split('/').pop() || '';
});

const getFileName = (path: string) => path.split('/').pop() || '';

const sendSftpCommand = (cmd: SFTP_CMD, data: any) => {
  if (!props.socket) return;

  const message = {
    id: uuid(),
    type: FILE_MANAGE_MESSAGE_TYPE.SFTP_DATA,
    cmd,
    data: JSON.stringify(data)
  };

  props.socket.send(JSON.stringify(message));
};

const handleRefresh = () => {
  emits('update:loading', true);

  const sendData = {
    path: props.currentNodePath
  };

  sendSftpCommand(SFTP_CMD.LIST, sendData);
};

const handleLayoutChange = (value: boolean) => {
  emits('update:layout', value);
};

const handleInputChange = (value: string) => {
  console.log(value);
};

const openModal = (type: ModalType, title: string, content: string = '') => {
  showModal.value = true;
  modalType.value = type;
  modalTitle.value = title;
  if (content) {
    modalContent.value = content;
  }
};

const handleCreateFolder = () => {
  openModal(ModalTypes.CREATE, t('NewFolder'));
};

const handleDelete = () => {
  openModal(ModalTypes.DELETE, 'Do you want to delete this file?');
};

const handleRename = () => {
  openModal(ModalTypes.RENAME, '重命名', getFileName(props.currentNodePath));
};

const handleModalConfirm = () => {
  switch (modalType.value) {
    case ModalTypes.DELETE:
      sendSftpCommand(SFTP_CMD.RM, {
        path: props.currentNodePath
      });
      break;

    case ModalTypes.CREATE:
      sendSftpCommand(SFTP_CMD.MKDIR, {
        path: `${props.currentNodePath}/${modalContent.value}`
      });
      break;

    case ModalTypes.RENAME:
      sendSftpCommand(SFTP_CMD.RENAME, {
        path: props.currentNodePath,
        new_name: modalContent.value
      });
      showModal.value = false;
      break;
  }
};

const leftActionsMenu = computed((): LeftActionsMenu[] => {
  return [
    {
      label: '新建文件夹',
      icon: FolderPlus,
      disabled: props.currentNodePath.length === 0,
      click: handleCreateFolder
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
      label: '刷新',
      icon: RefreshCcw,
      disabled: false,
      click: handleRefresh
    },
    {
      label: '删除',
      icon: Trash2,
      disabled: props.currentNodePath.length === 0 || props.currentNodePath === props.initialPath,
      click: handleDelete
    },
    {
      label: '重命名',
      icon: PenLine,
      disabled: props.currentNodePath.length === 0,
      click: handleRename
    }
  ];
});
</script>
