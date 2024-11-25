<template>
  <n-flex align="center" justify="space-between" class="!flex-nowrap !gap-x-10 h-[45px]">
    <n-flex class="controls-part !gap-x-6 h-full !flex-nowrap" align="center">
      <n-button text :disabled="disabledBack" @click="handlePathBack">
        <n-icon size="16" class="icon-hover" :component="ArrowBackIosFilled" />
      </n-button>

      <n-button text :disabled="disabledForward" @click="handlePathForward">
        <n-icon :component="ArrowForwardIosFilled" size="16" class="icon-hover" />
      </n-button>
    </n-flex>
    <n-flex class="action-part" align="center" justify="flex-end">
      <n-button size="small" type="info" secondary @click="handleNewFolder"> {{ t('newFolder') }} </n-button>
      <n-button size="small" type="info" secondary @click="handleNewFile"> {{ t('newFile') }} </n-button>
      <n-upload
        abstract
        :default-file-list="fileList"
        action="https://www.mocky.io/v2/5e4bafc63100007100d8b70f"
      >
        <n-button-group>
          <n-upload-trigger #="{ handleClick }" abstract>
            <n-button
              secondary
              round
              type="primary"
              size="small"
              @click="
                () => {
                  handleClick();
                  isShowUploadList = !isShowUploadList;
                }
              "
            >
              上传文件
            </n-button>
          </n-upload-trigger>
        </n-button-group>
        <n-card
          v-if="isShowUploadList"
          closable
          title="文件列表"
          class="absolute top-[3.5rem] right-2 z-[999999] w-[500px] h-[300px]"
        >
          <n-upload-file-list />
        </n-card>
      </n-upload>
      <n-button size="small" type="info" secondary @click="handleRefresh">
        <n-icon size="16" :component="Refresh" />
      </n-button>
    </n-flex>
  </n-flex>

  <n-flex class="file-part w-full h-10">
    <n-flex
      v-for="item of filePathList"
      :key="item.id"
      align="center"
      justify="center"
      class="file-node !flex-nowrap h-full"
    >
      <n-icon :component="Folder" size="18" :color="item.active ? '#63e2b7' : ''" />
      <n-text
        depth="1"
        class="text-[16px] cursor-pointer"
        :strong="item.active"
        @click="handlePathClick(item)"
      >
        {{ item.path }}
      </n-text>
      <n-icon v-if="item.showArrow" :component="ArrowForwardIosFilled" size="16" />
    </n-flex>
  </n-flex>

  <n-divider class="!my-[12px]" />

  <n-flex class="table-part">
    <n-data-table
      virtual-scroll
      size="small"
      :bordered="false"
      :max-height="1000"
      :columns="columns"
      :row-props="rowProps"
      :data="fileManageStore.fileList"
    />
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
  </n-flex>

  <n-modal
    v-model:show="showModal"
    preset="dialog"
    content="你确认?"
    positive-text="确认"
    negative-text="取消"
    :title="modalTitle"
    :content-style="{
      display: 'flex',
      alignItems: 'center',
      height: '100%',
      margin: '20px 0'
    }"
    @positive-click="modalPositiveClick"
    @negative-click="modalNegativeClick"
  >
    <n-input clearable v-model:value="newFileName" />
  </n-modal>
</template>

<script setup lang="ts">
import mittBus from '@/utils/mittBus.ts';

import { Folder, Refresh } from '@vicons/tabler';
import type { DataTableColumns, UploadFileInfo } from 'naive-ui';
import { NButton, NFlex, NIcon, NText, useMessage } from 'naive-ui';
import { ArrowBackIosFilled, ArrowForwardIosFilled } from '@vicons/material';

import { useI18n } from 'vue-i18n';
import { getFileName } from '@/utils';
import { getDropSelections } from './config.ts';
import { nextTick, onBeforeUnmount, ref, watch } from 'vue';
import { useFileManageStore } from '@/store/modules/fileManage.ts';
import { ManageTypes, unloadListeners } from '@/hooks/useFileManage.ts';

import type { RowData } from '@/components/pamFileList/index.vue';
import type { IFileManageSftpFileItem } from '@/hooks/interface';

export interface IFilePath {
  id: string;

  path: string;

  active: boolean;

  showArrow: boolean;
}

const props = withDefaults(
  defineProps<{
    columns: DataTableColumns<RowData>;
  }>(),
  {
    columns: () => []
  }
);

const emits = defineEmits<{
  (e: 'resetLoaded');
}>();

const { t } = useI18n();
const message = useMessage();
const options = getDropSelections(t);
const fileManageStore = useFileManageStore();

// TOdo)) 不用的后缀展示不同的文件 icon

const x = ref(0);
const y = ref(0);
const modalTitle = ref('');
const forwardPath = ref('');
const newFileName = ref('');
const showModal = ref(false);
const disabledBack = ref(true);
const showDropdown = ref(false);
const disabledForward = ref(true);
const isShowUploadList = ref(false);

const currentRowData = ref<RowData>();
const filePathList = ref<IFilePath[]>([]);
const fileList = ref<UploadFileInfo[]>([
  {
    id: 'b',
    name: 'file.doc',
    status: 'finished',
    type: 'text/plain'
  }
]);

watch(
  () => fileManageStore.currentPath,
  newPath => {
    if (newPath) {
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

const onClickOutside = () => {
  showDropdown.value = false;
};

const handleSelect = (key: string) => {
  showDropdown.value = false;

  switch (key) {
    case 'rename': {
      showModal.value = true;
      modalTitle.value = '重命名';

      break;
    }
    case 'delete': {
      break;
    }
    case 'download': {
      console.log(
        '%c DEBUG[ currentRowData ]-1:',
        'font-size:13px; background:#F0FFF0; color:#8B4513;',
        currentRowData.value
      );

      if (currentRowData.value?.is_dir) {
        mittBus.emit('download-file', {
          path: currentRowData.value.name,
          is_dir: currentRowData.value?.is_dir
        });
      }

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

    message.error('当前节点已为根节点！', { duration: 3000 });
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

const modalPositiveClick = () => {
  const index =
    fileManageStore?.fileList?.findIndex((item: IFileManageSftpFileItem) => {
      return item.name === newFileName.value;
    }) ?? -1;

  if (modalTitle.value === '重命名') {
    if (index !== -1) {
      message.error(`已存在 ${newFileName.value} 请重新命名`);

      nextTick(() => {
        newFileName.value = '';
        return (showModal.value = true);
      });
    } else {
      mittBus.emit('file-manage', {
        type: ManageTypes.RENAME,
        path: `${fileManageStore.currentPath}/${currentRowData.value.name}`,
        new_name: newFileName.value
      });

      newFileName.value = '';

      return;
    }
  } else {
    if (index !== -1) {
      return message.error('该文件已添加');
    } else {
      mittBus.emit('file-manage', {
        path: `${fileManageStore.currentPath}/${newFileName.value}`,
        type: ManageTypes.CREATE
      });

      newFileName.value = '';
    }
  }
};

const modalNegativeClick = () => {
  newFileName.value = '';
};

const handleNewFolder = () => {
  showModal.value = true;
  modalTitle.value = '创建文件夹';
};

const handleNewFile = () => {
  showModal.value = true;
  modalTitle.value = '创建文件';
};

const handleRefresh = () => {
  mittBus.emit('file-manage', { path: fileManageStore.currentPath, type: ManageTypes.REFRESH });
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

      mittBus.emit('file-manage', { path: splicePath, type: ManageTypes.CHANGE });
      disabledBack.value = false;
    }
  };
};

onBeforeUnmount(() => {
  unloadListeners();
});
</script>
