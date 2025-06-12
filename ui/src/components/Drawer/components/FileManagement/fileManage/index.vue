<template>
  <n-flex align="center" justify="space-between" vertical class="!gap-x-6">
    <n-flex align="center" class="w-full !flex-nowrap">
      <n-flex class="controls-part !gap-x-6 h-full !flex-nowrap" align="center">
        <n-button text :disabled="disabledBack" @click="handlePathBack">
          <n-icon size="16" class="icon-hover" :component="ArrowBackIosFilled" />
        </n-button>

        <n-button text :disabled="disabledForward" @click="handlePathForward">
          <n-icon :component="ArrowForwardIosFilled" size="16" class="icon-hover" />
        </n-button>
      </n-flex>

      <n-scrollbar ref="scrollRef" x-scrollable :content-style="{ height: '40px' }">
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
    </n-flex>

    <n-flex align="center" justify="space-between" class="w-full !flex-nowrap">
      <n-input v-model:value="searchValue" clearable size="small">
        <template #prefix>
          <Search :size="16" class="focus:outline-none" />
        </template>
      </n-input>

      <n-flex align="center" class="!flex-nowrap">
        <n-button secondary size="small" class="custom-button-text" @click="handleNewFolder">
          <template #icon>
            <n-icon :component="Plus" :size="12" />
          </template>
          {{ t('NewFolder') }}
        </n-button>

        <n-upload
          v-model:file-list="uploadFileList"
          abstract
          :multiple="false"
          :show-retry-button="false"
          :custom-request="customRequest"
          @remove="handleRemoveItem"
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
            v-model:show="showInner"
            resizable
            placement="bottom"
            :default-height="500"
            :trap-focus="false"
            :block-scroll="false"
            :native-scrollbar="false"
          >
            <n-drawer-content :title="t('TransferHistory')">
              <n-scrollbar v-if="uploadFileList" style="max-height: 400px">
                <n-upload-file-list />
              </n-scrollbar>

              <n-empty v-else class="w-full h-full justify-center" />
            </n-drawer-content>
          </n-drawer>
        </n-upload>

        <n-popover>
          <template #trigger>
            <n-icon
              size="16"
              :component="Refresh"
              class="icon-hover cursor-pointer text-white"
              @click="handleRefresh"
            />
          </template>
          {{ t('Refresh') }}
        </n-popover>

        <n-popover>
          <template #trigger>
            <n-icon
              size="16"
              :component="List"
              class="icon-hover cursor-pointer text-white"
              @click="handleOpenTransferList"
            />
          </template>
          {{ t('TransferHistory') }}
        </n-popover>
      </n-flex>
    </n-flex>
  </n-flex>

  <n-flex class="mt-4">
    <n-card size="small">
      <n-data-table
        remote
        single-line
        virtual-scroll
        size="small"
        :bordered="false"
        :loading="loading"
        :max-height="1000"
        :columns="columns"
        :row-props="rowProps"
        :data="dataList"
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
    </n-card>
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
    <n-input v-if="!modalContent" v-model:value="newFileName" clearable />
  </n-modal>
</template>

<script setup lang="ts">
import mittBus from '@/utils/mittBus';

import { List } from '@vicons/ionicons5';
import { Search } from 'lucide-vue-next';
import { Folder, Refresh, Plus } from '@vicons/tabler';
import { NButton, NFlex, NIcon, NText, UploadCustomRequestOptions, useMessage } from 'naive-ui';
import { ArrowBackIosFilled, ArrowForwardIosFilled } from '@vicons/material';

import { useI18n } from 'vue-i18n';
import { getFileName } from '@/utils';
import { getDropSelections } from './config.tsx';
import { nextTick, onBeforeUnmount, onMounted, ref, watch, onActivated, provide } from 'vue';
import { useFileManageStore } from '@/store/modules/fileManage.ts';
import { ManageTypes, unloadListeners } from '@/hooks/useFileManage.ts';

import type { FileManageSftpFileItem } from '@/types/modules/file.type';
import type { DataTableColumns, UploadFileInfo } from 'naive-ui';
import type { RowData } from '@/components/Drawer/components/FileManagement/index.vue';

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
const searchValue = ref('');
const modalContent = ref('');
const loading = ref(false);
const showInner = ref(false);
const showModal = ref(false);
const showDropdown = ref(false);
const isShowUploadList = ref(false);
const disabledBack = ref(true);
const disabledForward = ref(true);

const scrollRef = ref(null);
const dataList = ref<any[]>([]);

const filePathList = ref<IFilePath[]>([]);
const currentRowData = ref<Partial<RowData>>({});
const persistedUploadFiles = ref<UploadFileInfo[]>([]);
const uploadFileList = ref<UploadFileInfo[]>([]);
const stopUploadFile = ref<UploadFileInfo>();

watch(
  () => fileManageStore.currentPath,
  newPath => {
    if (newPath) {
      // 重置现有路径列表
      filePathList.value = [];

      if (newPath === '/') {
        disabledBack.value = true;
        return;
      }

      if (fileManageStore.currentPath === forwardPath.value) {
        disabledForward.value = true;
      }

      // 分割路径
      const pathSegments = newPath.split('/').filter(segment => segment);

      // 根据路径段构建完整的路径列表
      let currentPath = '';
      pathSegments.forEach((segment, index) => {
        // 更新当前路径
        currentPath += '/' + segment;

        // 添加到路径列表
        filePathList.value.push({
          id: currentPath, // 使用完整路径作为ID
          path: segment, // 显示路径段名称
          active: index === pathSegments.length - 1,
          showArrow: index !== pathSegments.length - 1
        });
      });

      // 滚动到最后一个路径段
      nextTick(() => {
        const contentRef = document.getElementsByClassName('n-scrollbar-content')[2];
        if (scrollRef.value && contentRef) {
          // @ts-ignore
          scrollRef.value.scrollTo({
            left: contentRef.scrollWidth,
            behavior: 'smooth'
          });
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
      dataList.value = newFileList;
    }
  },
  {
    immediate: true
  }
);

watch(
  () => uploadFileList.value,
  newValue => {
    if (newValue && newValue.length > 0) {
      persistedUploadFiles.value = [...newValue];
    }
  },
  { deep: true }
);

watch(
  () => searchValue.value,
  (newVal: string) => {
    if (newVal) {
      dataList.value = fileManageStore.fileList!.filter(item => item.name.toLowerCase().includes(newVal.toLowerCase()));
    } else {
      dataList.value = fileManageStore.fileList!;
    }
  }
);

const onClickOutside = () => {
  showDropdown.value = false;
};

const handleRemoveItem = (data: { file: UploadFileInfo; fileList: UploadFileInfo[] }) => {
  mittBus.emit('stop-upload', { fileInfo: data.file });

  return false;
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
        is_dir: currentRowData.value.is_dir!,
        size: currentRowData.value.size!
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
  searchValue.value = '';

  // 保存当前路径用于前进导航
  disabledForward.value = false;
  forwardPath.value = fileManageStore.currentPath;

  const backPath = removeLastPathSegment(fileManageStore.currentPath);

  // 如果返回到根目录，设置后退按钮为禁用
  if (backPath === '' || backPath === '/') {
    disabledBack.value = true;
  }

  mittBus.emit('file-manage', {
    path: backPath || '/',
    type: ManageTypes.CHANGE
  });
};

/**
 * @description 前进
 */
const handlePathForward = () => {
  searchValue.value = '';

  if (forwardPath.value !== fileManageStore.currentPath) {
    disabledBack.value = false;

    const currentSegments = fileManageStore.currentPath.split('/');
    const forwardSegments = forwardPath.value.split('/');

    if (forwardSegments.length > currentSegments.length) {
      // 移除多余的第一个路径段
      const firstExtraSegment = forwardSegments.slice(currentSegments.length)[0];

      const newForwardPath = `${fileManageStore.currentPath}/${firstExtraSegment}`;

      mittBus.emit('file-manage', {
        path: newForwardPath,
        type: ManageTypes.CHANGE
      });
    }
  }
};

/**
 * @description 鼠标手动跳转
 */
const handlePathClick = (item: IFilePath) => {
  searchValue.value = '';

  // 如果点击了当前活动的路径段，不执行任何操作
  if (item.active) return;

  // 保存当前路径用于前进导航
  disabledForward.value = false;
  forwardPath.value = fileManageStore.currentPath;

  // 直接使用完整路径ID进行导航
  mittBus.emit('file-manage', { path: item.id, type: ManageTypes.CHANGE });
};

/**
 * @description 刷新
 */
const handleRefresh = () => {
  loading.value = true;
  mittBus.emit('file-manage', {
    path: fileManageStore.currentPath,
    type: ManageTypes.REFRESH
  });
};

/**
 * @description modal 对话框
 */
const modalPositiveClick = () => {
  const index =
    fileManageStore?.fileList?.findIndex((item: FileManageSftpFileItem) => {
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

  // TODO 提示
  if (modalType.value === 'stop') {
    loading.value = true;

    mittBus.emit('stop-upload', { fileInfo: stopUploadFile.value! });
  }
};

/**
 * @description 文件上传
 */
const handleUploadFileChange = (options: { fileList: Array<UploadFileInfo> }) => {
  showInner.value = true;

  if (options.fileList.length > 0) {
    uploadFileList.value = options.fileList;
    fileManageStore.setUploadFileList(options.fileList);
  }
};

/**
 * @description 自定义上传
 * @param onFinish
 * @param onError
 * @param onProgress
 */
const customRequest = ({ onFinish, onError, onProgress }: UploadCustomRequestOptions) => {
  // 创建loading消息
  const loadingMessage = message.loading(`${t('UploadProgress')}: 0%`, { duration: 1000000000 });

  mittBus.emit('file-upload', {
    uploadFileList,
    onFinish: () => {
      onFinish();
      loadingMessage.destroy();
    },
    onError: () => {
      onError();
      loadingMessage.destroy();
    },
    onProgress,
    loadingMessage
  });
};

/**
 * @description 打开传输历史列表
 */
const handleOpenTransferList = () => {
  // 从 store 中恢复文件列表
  if (fileManageStore.uploadFileList.length > 0) {
    uploadFileList.value = [...fileManageStore.uploadFileList];
  }
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
      searchValue.value = '';

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

        mittBus.emit('file-manage', {
          path: backPath,
          type: ManageTypes.CHANGE
        });

        handlePathBack();

        return;
      }

      mittBus.emit('file-manage', {
        path: splicePath,
        type: ManageTypes.CHANGE
      });

      disabledBack.value = false;
    }
  };
};

onMounted(() => {
  mittBus.on('reload-table', handleTableLoading);

  if (fileManageStore.uploadFileList.length > 0) {
    uploadFileList.value = [...fileManageStore.uploadFileList];
  }
});

onBeforeUnmount(() => {
  unloadListeners();

  mittBus.off('reload-table', handleTableLoading);
});

onActivated(() => {
  if (persistedUploadFiles.value.length > 0) {
    uploadFileList.value = [...persistedUploadFiles.value];
  }
});

provide('persistedUploadFiles', persistedUploadFiles);
</script>
