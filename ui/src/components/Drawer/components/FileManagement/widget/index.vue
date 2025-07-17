<script setup lang="ts">
import type { DataTableColumns, DropdownOption, UploadCustomRequestOptions, UploadFileInfo } from 'naive-ui';

import { useI18n } from 'vue-i18n';
import { NText, useMessage } from 'naive-ui';
import { useWindowSize } from '@vueuse/core';
import { computed, h, nextTick, onActivated, onBeforeUnmount, onMounted, provide, ref, watch } from 'vue';
import {
  ChevronLeft,
  ChevronRight,
  Download,
  Folder,
  PenLine,
  Plus,
  RefreshCcw,
  Search,
  Trash,
  Upload,
} from 'lucide-vue-next';

import type { FileManageSftpFileItem } from '@/types/modules/file.type';
import type { RowData } from '@/components/Drawer/components/FileManagement/index.vue';

import { getFileName } from '@/utils';
import mittBus from '@/utils/mittBus';
import { useFileManageStore } from '@/store/modules/fileManage.ts';
import { ManageTypes, unloadListeners } from '@/hooks/useFileManage.ts';

export interface IFilePath {
  id: string;

  path: string;

  active: boolean;

  showArrow: boolean;
}

withDefaults(
  defineProps<{
    columns?: DataTableColumns<RowData>;
  }>(),
  {
    columns: () => [],
  }
);

const { t } = useI18n();
const message = useMessage();
const fileManageStore = useFileManageStore();
const { height: _windowHeight } = useWindowSize();

const options: DropdownOption[] = [
  {
    key: 'rename',
    label: t('Rename'),
    icon: () => h(PenLine, { size: 16 }),
  },
  {
    key: 'download',
    label: t('Download'),
    icon: () => h(Download, { size: 16 }),
  },
  {
    type: 'divider',
    key: 'd1',
  },
  {
    key: 'delete',
    icon: () => h(Trash, { size: 16, color: '#ff6b6b' }),
    label: () => h(NText, { depth: 1, style: { color: '#ff6b6b' } }, { default: () => t('Delete') }),
  },
];

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

const _tableHeight = computed(() => {
  if (!uploadFileList.value || uploadFileList.value.length === 0) {
    return 240;
  }
  return 300;
});

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
        currentPath += `/${segment}`;

        // 添加到路径列表
        filePathList.value.push({
          id: currentPath, // 使用完整路径作为ID
          path: segment, // 显示路径段名称
          active: index === pathSegments.length - 1,
          showArrow: index !== pathSegments.length - 1,
        });
      });

      // 滚动到最后一个路径段
      nextTick(() => {
        const contentRef = document.getElementsByClassName('n-scrollbar-content')[2];
        if (scrollRef.value && contentRef) {
          // @ts-expect-error 目标对象滚动
          scrollRef.value.scrollTo({
            left: contentRef.scrollWidth,
            behavior: 'smooth',
          });
        }
      });
    }
  },
  {
    immediate: true,
  }
);

watch(
  () => forwardPath.value,
  (newPath, oldPath) => {
    if (oldPath && (oldPath === newPath || oldPath.startsWith(`${newPath}/`))) {
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
    immediate: true,
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
  const { file, fileList } = data;

  // 如果文件正在上传中，发送停止上传事件
  if (file.status === 'uploading') {
    mittBus.emit('stop-upload', { fileInfo: file });
    // 对于正在上传的文件，暂时不移除，等待停止上传完成后再移除
    return false;
  }

  // 对于上传失败、已完成或其他状态的文件，允许直接移除
  uploadFileList.value = fileList.filter(item => item.id !== file.id);
  fileManageStore.setUploadFileList(uploadFileList.value);

  return true;
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
        size: currentRowData.value.size!,
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
  // 移除末尾的斜杠
  if (path.endsWith('/')) {
    path = path.slice(0, -1);
  }

  // 如果是根目录，返回空字符串
  if (path === '') {
    return '';
  }

  // 找到最后一个斜杠的位置
  const lastSlashIndex = path.lastIndexOf('/');

  // 如果没有找到斜杠，返回空字符串
  if (lastSlashIndex === -1) {
    return '';
  }

  // 如果斜杠在开头（根目录的情况），返回根目录
  if (lastSlashIndex === 0) {
    return '/';
  }

  // 返回去掉最后一个路径段的结果
  return path.substring(0, lastSlashIndex);
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
    type: ManageTypes.CHANGE,
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
        type: ManageTypes.CHANGE,
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
    type: ManageTypes.REFRESH,
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
      message.error(`${newFileName.value} ${t('AlreadyExistsPleaseRename')}`);

      nextTick(() => {
        newFileName.value = '';
        return (showModal.value = true);
      });
    } else {
      loading.value = true;

      mittBus.emit('file-manage', {
        type: ManageTypes.RENAME,
        path: `${fileManageStore.currentPath}/${currentRowData?.value?.name}`,
        new_name: newFileName.value,
      });

      newFileName.value = '';

      return;
    }
  }

  if (modalType.value === 'delete') {
    loading.value = true;

    mittBus.emit('file-manage', {
      type: ManageTypes.REMOVE,
      path: `${fileManageStore.currentPath}/${currentRowData?.value?.name}`,
    });

    nextTick(() => {
      modalTitle.value = '';
      modalContent.value = '';
    });
  }

  if (modalType.value === 'add') {
    if (index !== -1) {
      return message.error(t('FileAlreadyExists'));
    } else {
      loading.value = true;

      mittBus.emit('file-manage', {
        path: `${fileManageStore.currentPath}/${newFileName.value}`,
        type: ManageTypes.CREATE,
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
  if (options.fileList.length > 0) {
    uploadFileList.value = options.fileList;
    fileManageStore.setUploadFileList(options.fileList);

    // 使用 nextTick 确保数据更新后再打开抽屉
    nextTick(() => {
      showInner.value = true;
    });
  }
};

/**
 * @description 自定义上传
 */
const customRequest = ({ file, onFinish, onError, onProgress }: UploadCustomRequestOptions) => {
  const loadingMessage = message.loading(`${t('UploadProgress')}: 0%`, { duration: 1000000000 });

  mittBus.emit('file-upload', {
    uploadFileList,
    onFinish: () => {
      onFinish();
      loadingMessage.destroy();

      // 文件上传成功后，5秒后自动移除
      setTimeout(() => {
        uploadFileList.value = uploadFileList.value.filter(item => item.id !== file.id);
        fileManageStore.setUploadFileList(uploadFileList.value);
      }, 5000);
    },
    onError: () => {
      onError();
      loadingMessage.destroy();
    },
    onProgress,
    loadingMessage,
  });
};

/**
 * @description 打开传输历史列表
 */
// const handleOpenTransferList = () => {
// 从 store 中恢复文件列表
//   uploadFileList.value = [...fileManageStore.uploadFileList];

//   nextTick(() => {
//     showInner.value = true;
//   });
// };

const modalNegativeClick = () => {
  newFileName.value = '';
};

const handleNewFolder = () => {
  modalType.value = 'add';
  showModal.value = true;
  modalTitle.value = t('CreateFolder');
};

const handleTableLoading = () => {
  loading.value = true;
  mittBus.emit('file-manage', {
    path: fileManageStore.currentPath,
    type: ManageTypes.REFRESH,
  });
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
        // return message.error('暂不支持文件预览');
        return;
      }

      if (row.name === '..') {
        const backPath = removeLastPathSegment(fileManageStore.currentPath) || '/';

        // 更新前进路径用于前进导航
        disabledForward.value = false;
        forwardPath.value = fileManageStore.currentPath;

        // 如果返回到根目录，设置后退按钮为禁用
        if (backPath === '/') {
          disabledBack.value = true;
        }

        mittBus.emit('file-manage', {
          path: backPath,
          type: ManageTypes.CHANGE,
        });

        return;
      }

      mittBus.emit('file-manage', {
        path: splicePath,
        type: ManageTypes.CHANGE,
      });

      disabledBack.value = false;
    },
  };
};

onMounted(() => {
  mittBus.on('reload-table', handleTableLoading);

  // 监听上传停止成功事件，移除对应的文件
  mittBus.on('upload-stopped', (data: { fileInfo: UploadFileInfo }) => {
    uploadFileList.value = uploadFileList.value.filter(item => item.id !== data.fileInfo.id);
    fileManageStore.setUploadFileList(uploadFileList.value);
  });

  if (fileManageStore.uploadFileList.length > 0) {
    uploadFileList.value = [...fileManageStore.uploadFileList];
  }
});

onBeforeUnmount(() => {
  unloadListeners();

  mittBus.off('reload-table', handleTableLoading);
  mittBus.off('upload-stopped');
});

onActivated(() => {
  if (persistedUploadFiles.value.length > 0) {
    uploadFileList.value = [...persistedUploadFiles.value];
  }
});

provide('persistedUploadFiles', persistedUploadFiles);
</script>

<template>
  <n-flex align="center" justify="space-between" vertical class="!gap-x-6">
    <n-flex align="center" class="w-full !flex-nowrap">
      <n-flex class="controls-part !gap-x-6 h-full !flex-nowrap" align="center">
        <n-button text :disabled="disabledBack" @click="handlePathBack">
          <ChevronLeft :size="16" class="icon-hover" />
        </n-button>

        <n-button text :disabled="disabledForward" @click="handlePathForward">
          <ChevronRight :size="16" class="icon-hover" />
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
            <Folder :size="18" :color="item.active ? '#63e2b7' : ''" class="text-white" />
            <NText
              depth="1"
              class="text-[16px] cursor-pointer whitespace-nowrap"
              :strong="item.active"
              @click="handlePathClick(item)"
            >
              {{ item.path }}
            </NText>

            <ChevronRight v-if="item.showArrow" :size="16" class="text-white" />
          </n-flex>
        </n-flex>
      </n-scrollbar>
    </n-flex>

    <n-flex align="center" justify="space-between" class="w-full !flex-nowrap">
      <n-input v-model:value="searchValue" clearable size="small" placeholder="">
        <template #prefix>
          <Search :size="16" class="focus:outline-none" />
        </template>
      </n-input>

      <n-flex align="center" class="!flex-nowrap">
        <n-button secondary size="small" class="custom-button-text" @click="handleNewFolder">
          <template #icon>
            <Plus :size="12" />
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
                <template #icon>
                  <NIcon :component="Upload" :size="12" />
                </template>

                {{ t('UploadTitle') }}
              </n-button>
            </n-upload-trigger>
          </n-button-group>

          <!-- <n-drawer
            v-model:show="showInner"
            resizable
            placement="bottom"
            :default-height="drawerHeight"
            :max-height="drawerHeight"
            :show-mask="false"
            :trap-focus="false"
            :block-scroll="false"
            :native-scrollbar="false"
            :height="300"
            to="#drawer-inner-target"
          >
            <n-drawer-content
              closable
              :title="t('TransferHistory')"
              :body-style="{
                overflow: 'unset',
                height: '100%',
                display: 'flex',
                flexDirection: 'column',
              }"
            >
              <n-scrollbar v-if="uploadFileList" :style="{ maxHeight: `${drawerHeight - 60}px`, flex: 1 }">

              </n-scrollbar>

              <n-empty v-else class="w-full h-full justify-center" />
            </n-drawer-content>
          </n-drawer> -->
        </n-upload>

        <!-- <n-popover>
          <template #trigger>
            <ListTree
              :size="16"
              class="icon-hover cursor-pointer !text-white focus:outline-none"
              @click="handleOpenTransferList"
            />
          </template>
          {{ t('Transfer') }}
        </n-popover> -->

        <n-popover>
          <template #trigger>
            <RefreshCcw
              :size="16"
              class="icon-hover cursor-pointer !text-white focus:outline-none"
              @click="handleRefresh"
            />
          </template>
          {{ t('Refresh') }}
        </n-popover>
      </n-flex>
    </n-flex>
  </n-flex>

  <n-flex class="mt-4">
    <n-card size="small">
      <n-data-table
        remote
        single-line
        flex-height
        virtual-scroll
        size="small"
        :bordered="false"
        :loading="loading"
        :columns="columns"
        :row-props="rowProps"
        :data="dataList"
        :style="{ height: `calc(100vh - 420px)` }"
      >
        <template #empty>
          <n-empty class="w-full h-full justify-center" :description="t('NoData')" />
        </template>
      </n-data-table>

      <n-dropdown
        size="small"
        trigger="manual"
        placement="bottom-start"
        :x="x"
        :y="y"
        :show-arrow="true"
        :options="options"
        :show="showDropdown"
        :on-clickoutside="onClickOutside"
        @select="handleSelect"
      />

      <template v-if="uploadFileList.length > 0" #footer>
        <n-divider />
        <n-flex vertical class="w-full">
          <n-upload
            abstract
            file-list-class="max-height-32"
            :show-preview-button="false"
            :show-retry-button="false"
            :file-list="uploadFileList"
            @remove="handleRemoveItem"
          >
            <n-upload-file-list />
          </n-upload>
        </n-flex>
      </template>
    </n-card>
  </n-flex>

  <n-modal
    v-model:show="showModal"
    preset="dialog"
    :title="modalTitle"
    :show-icon="false"
    :content="modalContent"
    :positive-text="t('Confirm')"
    :type="modalContent ? 'error' : 'success'"
    :content-style="{
      display: 'flex',
      alignItems: 'center',
      height: '100%',
      margin: '20px 0',
    }"
    @positive-click="modalPositiveClick"
    @negative-click="modalNegativeClick"
  >
    <n-input v-if="!modalContent" v-model:value="newFileName" clearable :placeholder="t('PleaseInput')" />
  </n-modal>
</template>

<style scoped lang="scss">
:deep(.n-drawer .n-drawer-content .n-drawer-body) {
  overflow: unset !important;
}
</style>
