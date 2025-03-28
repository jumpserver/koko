<template>
  <n-flex align="center" justify="space-between" class="!flex-nowrap !gap-x-6 h-[45px]">
    <n-flex class="controls-part !gap-x-6 h-full !flex-nowrap flex-1" align="center">
      <n-button text :disabled="disabledBack" @click="handlePathBack">
        <n-icon size="16" class="icon-hover" :component="ArrowBackIosFilled" />
      </n-button>

      <n-button text :disabled="disabledForward" @click="handlePathForward">
        <n-icon :component="ArrowForwardIosFilled" size="16" class="icon-hover" />
      </n-button>
    </n-flex>

    <n-scrollbar
      x-scrollable
      ref="scrollRef"
      class="flex flex-2 items-center"
      :content-style="{ height: '100%' }"
    >
      <n-flex class="file-part w-full h-full !flex-nowrap">
        <n-flex
          v-for="item of filePathList"
          :key="item.id"
          align="center"
          justify="flex-start"
          class="file-node !flex-nowrap"
        >
          <n-icon :component="Folder" size="18" :color="item.active ? '#63e2b7' : ''" />
          <n-text
            depth="1"
            class="text-[16px] cursor-pointer whitespace-nowrap"
            :strong="item.active"
            @click="handlePathClick(item)"
          >
            {{ item.path }}
          </n-text>
          <n-icon v-if="item.showArrow" :component="ArrowForwardIosFilled" size="16" />
        </n-flex>
      </n-flex>
    </n-scrollbar>

    <n-flex class="action-part !flex-nowrap flex-2" align="center" justify="flex-end">
      <n-button 
        secondary 
        size="small"
        class="custom-button-text"
        @click="handleNewFolder"
      >
        <template #icon>
          <n-icon :component="Plus" :size="12" />
        </template>
        {{ t('NewFolder') }}
      </n-button>

      <n-upload
        abstract
        :multiple="false"
        :show-retry-button="false"
        :custom-request="customRequest"
        v-model:file-list="uploadFileList"
        @change="handleUploadFileChange"
      >
        <n-button-group>
          <n-upload-trigger #="{ handleClick }" abstract>
            <n-button
              secondary
              size="small"
              class="custom-button-text"
              @click="
                () => {
                  handleClick();
                  isShowUploadList = !isShowUploadList;
                }
              "
            >
              {{ t('UploadTitle') }}
            </n-button>
          </n-upload-trigger>
        </n-button-group>

        <n-drawer
          resizable
          placement="bottom"
          to="#drawer-inner-target"
          :default-height="500"
          :trap-focus="false"
          :block-scroll="false"
          :native-scrollbar="false"
          v-model:show="showInner"
        >
          <n-drawer-content :title="t('TransferHistory')">
            <n-scrollbar style="max-height: 400px" v-if="uploadFileList">
              <n-upload-file-list />
            </n-scrollbar>

            <n-empty v-else class="w-full h-full justify-center" />
          </n-drawer-content>
        </n-drawer>
      </n-upload>

      <n-popover>
        <template #trigger>
          <n-icon size="16" :component="Refresh" class="icon-hover" @click="handleRefresh" />
        </template>
        {{ t('Refresh') }}
      </n-popover>

      <n-popover>
        <template #trigger>
          <n-icon size="16" :component="List" class="icon-hover" @click="handleOpenTransferList" />
        </template>
        {{ t('TransferHistory') }}
      </n-popover>
    </n-flex>
  </n-flex>

  <n-divider class="!my-[12px]" />

  <n-flex class="table-part">
    <n-data-table
      remote
      virtual-scroll
      size="small"
      :bordered="false"
      :loading="loading"
      :max-height="1000"
      :columns="columns"
      :row-props="rowProps"
      :data="fileManageStore.fileList"
    />
    <n-dropdown
      size="small"
      trigger="manual"
      placement="bottom-start"
      class="w-[8rem]"
      :x="x"
      :y="y"
      :show-arrow="true"
      :options="options"
      :show="showDropdown"
      :on-clickoutside="onClickOutside"
      @select="handleSelect"
    />
  </n-flex>

  <n-modal
    v-model:show="showModal"
    preset="dialog"
    :title="modalTitle"
    :content="modalContent"
    :positive-text="t('Confirm')"
    :negative-text="t('Cancel')"
    :type="modalContent ? 'error' : 'success'"
    :content-style="{
      display: 'flex',
      alignItems: 'center',
      height: '100%',
      margin: '20px 0'
    }"
    @positive-click="modalPositiveClick"
    @negative-click="modalNegativeClick"
  >
    <n-input v-if="!modalContent" clearable v-model:value="newFileName" />
  </n-modal>
</template>

<script setup lang="ts">
import mittBus from '@/utils/mittBus.ts';

import { List } from '@vicons/ionicons5';
import { Folder, Refresh, Plus } from '@vicons/tabler';
import { NButton, NFlex, NIcon, NText, UploadCustomRequestOptions, useMessage } from 'naive-ui';
import { ArrowBackIosFilled, ArrowForwardIosFilled } from '@vicons/material';

import { useI18n } from 'vue-i18n';
import { getFileName } from '@/utils';
import { getDropSelections } from './config.tsx';
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useFileManageStore } from '@/store/modules/fileManage.ts';
import { ManageTypes, unloadListeners } from '@/hooks/useFileManage.ts';

import type { RowData } from '@/components/FileManagement/index.vue';
import type { IFileManageSftpFileItem } from '@/hooks/interface';
import type { DataTableColumns, UploadFileInfo } from 'naive-ui';

export interface IFilePath {
  id: string;

  path: string;

  active: boolean;

  showArrow: boolean;
}

withDefaults(
  defineProps<{
    columns: DataTableColumns<RowData>;
  }>(),
  {
    columns: () => []
  }
);

const { t } = useI18n();
const message = useMessage();
const options = getDropSelections(t);
const fileManageStore = useFileManageStore();

const x = ref(0);
const y = ref(0);
const modalType = ref('');
const modalTitle = ref('');
const forwardPath = ref('');
const newFileName = ref('');
const modalContent = ref('');
const loading = ref(false);
const showInner = ref(false);
const showModal = ref(false);
const showDropdown = ref(false);
const isShowUploadList = ref(false);
const disabledBack = ref(true);
const disabledForward = ref(true);

const scrollRef = ref(null);

const currentRowData = ref<RowData>();
const filePathList = ref<IFilePath[]>([]);
const uploadFileList = ref<UploadFileInfo[]>([]);

watch(
  () => fileManageStore.currentPath,
  newPath => {
    if (newPath) {
      if (newPath === '/') {
        filePathList.value = [];

        disabledBack.value = true;
        filePathList.value.push({
          id: '/',
          path: '/',
          active: true,
          showArrow: false
        });

        return;
      }

      // 重置所有项的 active 和 showArrow 状态
      filePathList.value.forEach(item => {
        item.active = false;
        item.showArrow = true;
      });

      if (fileManageStore.currentPath === forwardPath.value) {
        disabledForward.value = true;
      }

      const pathSegments = newPath.split('/');

      pathSegments.forEach((segment, index) => {
        if (segment) {
          const existingItem = filePathList.value.find(item => item.path === segment);

          if (!existingItem) {
            filePathList.value.push({
              id: segment,
              path: segment,
              active: index === pathSegments.length - 1,
              showArrow: index !== pathSegments.length - 1
            });

            nextTick(() => {
              const contentRef = document.getElementsByClassName('n-scrollbar-content')[2];

              if (scrollRef.value) {
                // @ts-ignore
                scrollRef.value.scrollTo({
                  left: contentRef.scrollWidth + 299,
                  behavior: 'smooth'
                });
              }
            });
          } else {
            // 如果段已经存在，更新其状态
            existingItem.active = index === pathSegments.length - 1;
            existingItem.showArrow = index !== pathSegments.length - 1;
          }
        }
      });
    }
  },
  {
    immediate: true
  }
);

watch(
  () => forwardPath.value,
  (newPath, oldPath) => {
    if (oldPath && (oldPath === newPath || oldPath.startsWith(newPath + '/'))) {
      // 如果 oldPath 包含 newPath，则重置 forwardPath 为 oldPath
      forwardPath.value = oldPath;
    }
  }
);

watch(
  () => fileManageStore.fileList,
  newFileList => {
    if (newFileList) {
      loading.value = false;
    }
  },
  {
    immediate: true
  }
);

const onClickOutside = () => {
  showDropdown.value = false;
};

/**
 * @description dropdown 的 select 回调
 * @param key
 */
const handleSelect = (key: string) => {
  showDropdown.value = false;

  switch (key) {
    case 'rename': {
      modalType.value = 'rename';
      showModal.value = true;
      modalTitle.value = t('Rename');

      break;
    }
    case 'delete': {
      modalType.value = 'delete';
      showModal.value = true;
      modalTitle.value = t('ConfirmDelete');
      modalContent.value = t('DangerWarning');
      break;
    }
    case 'download': {
      mittBus.emit('download-file', {
        path: `${fileManageStore.currentPath}/${currentRowData?.value?.name as string}`,
        is_dir: currentRowData.value?.is_dir as boolean
      });

      break;
    }
  }
};

/**
 * @description 返回按钮对路径的预处理，用于移除最后的 /xxx
 * @param path
 */
const removeLastPathSegment = (path: string): string => {
  if (path.endsWith('/')) {
    path = path.slice(0, -1);
  }

  const lastSegmentMatch = path.match(/\/([^\/]+)\/?$/);

  if (lastSegmentMatch) {
    return path.replace(lastSegmentMatch[0], '');
  } else {
    return '';
  }
};

/**
 * @description 后退
 */
const handlePathBack = () => {
  disabledForward.value = false;
  forwardPath.value = fileManageStore.currentPath;

  const backPath = removeLastPathSegment(fileManageStore.currentPath);

  mittBus.emit('file-manage', { path: backPath, type: ManageTypes.CHANGE });

  if (filePathList.value.length > 1) {
    filePathList.value.splice(filePathList.value.length - 1, 1);
  } else {
    disabledBack.value = true;

    // message.error('当前节点已为根节点！', { duration: 3000 });
  }
};

/**
 * @description 前进
 */
const handlePathForward = () => {
  if (forwardPath.value !== fileManageStore.currentPath) {
    disabledBack.value = false;

    const currentSegments = fileManageStore.currentPath.split('/');
    const forwardSegments = forwardPath.value.split('/');

    if (forwardSegments.length > currentSegments.length) {
      // 移除多余的第一个路径段
      const firstExtraSegment = forwardSegments.slice(currentSegments.length)[0];

      const newForwardPath = `${fileManageStore.currentPath}/${firstExtraSegment}`;

      mittBus.emit('file-manage', { path: newForwardPath, type: ManageTypes.CHANGE });
    }
  }
};

/**
 * @description 鼠标手动跳转
 */
const handlePathClick = (item: IFilePath) => {
  const path = item.path;

  mittBus.emit('file-manage', { path, type: ManageTypes.CHANGE });
};

/**
 * @description 刷新
 */
const handleRefresh = () => {
  loading.value = true;
  mittBus.emit('file-manage', { path: fileManageStore.currentPath, type: ManageTypes.REFRESH });
};

/**
 * @description modal 对话框
 */
const modalPositiveClick = () => {
  const index =
    fileManageStore?.fileList?.findIndex((item: IFileManageSftpFileItem) => {
      return item.name === newFileName.value;
    }) ?? -1;

  if (modalType.value === 'rename') {
    if (index !== -1) {
      message.error(`已存在 ${newFileName.value} 请重新命名`);

      nextTick(() => {
        newFileName.value = '';
        return (showModal.value = true);
      });
    } else {
      loading.value = true;

      mittBus.emit('file-manage', {
        type: ManageTypes.RENAME,
        path: `${fileManageStore.currentPath}/${currentRowData?.value?.name}`,
        new_name: newFileName.value
      });

      newFileName.value = '';

      return;
    }
  }

  if (modalType.value === 'delete') {
    loading.value = true;

    mittBus.emit('file-manage', {
      type: ManageTypes.REMOVE,
      path: `${fileManageStore.currentPath}/${currentRowData?.value?.name}`
    });

    nextTick(() => {
      modalTitle.value = '';
      modalContent.value = '';
    });
  }

  if (modalType.value === 'add') {
    if (index !== -1) {
      return message.error('该文件已存在');
    } else {
      loading.value = true;

      mittBus.emit('file-manage', {
        path: `${fileManageStore.currentPath}/${newFileName.value}`,
        type: ManageTypes.CREATE
      });

      newFileName.value = '';
    }
  }
};

/**
 * @description 文件上传
 */
const handleUploadFileChange = (options: { fileList: Array<UploadFileInfo> }) => {
  showInner.value = true;

  if (options.fileList.length > 0) {
    uploadFileList.value = options.fileList;
  }
};

/**
 * @description 自定义上传
 * @param onFinish
 * @param onError
 * @param onProgress
 */
const customRequest = ({ onFinish, onError, onProgress }: UploadCustomRequestOptions) => {
  mittBus.emit('file-upload', { uploadFileList, onFinish, onError, onProgress });
};

const handleOpenTransferList = () => {
  showInner.value = true;
};

const modalNegativeClick = () => {
  newFileName.value = '';
};

const handleNewFolder = () => {
  modalType.value = 'add';
  showModal.value = true;
  modalTitle.value = '创建文件夹';
};

const handleTableLoading = () => {
  loading.value = false;

  handleRefresh();
};

const rowProps = (row: RowData) => {
  return {
    style: 'cursor: pointer',
    onContextmenu: (e: MouseEvent) => {
      currentRowData.value = row;

      e.preventDefault();

      showDropdown.value = false;

      nextTick().then(() => {
        showDropdown.value = true;
        x.value = e.clientX;
        y.value = e.clientY;
      });
    },
    onClick: () => {
      const suffix = getFileName(row);
      const splicePath = `${fileManageStore.currentPath}/${row.name}`;

      if (suffix !== 'Folder') {
        return message.error('暂不支持文件预览');
      }

      if (row.name === '..') {
        const backPath = removeLastPathSegment(fileManageStore.currentPath)
          ? removeLastPathSegment(fileManageStore.currentPath)
          : '/';

        if (backPath === '/' && filePathList.value.findIndex(item => item.path === '/') === -1) {
          fileManageStore.setCurrentPath('/');
        }

        mittBus.emit('file-manage', { path: backPath, type: ManageTypes.CHANGE });

        handlePathBack();

        return;
      }

      mittBus.emit('file-manage', { path: splicePath, type: ManageTypes.CHANGE });

      disabledBack.value = false;
    }
  };
};

onMounted(() => {
  mittBus.on('reload-table', handleTableLoading);
});

onBeforeUnmount(() => {
  unloadListeners();

  mittBus.off('reload-table', handleTableLoading);
});
</script>
