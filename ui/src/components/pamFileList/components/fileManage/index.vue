<template>
  <n-flex align="center" justify="flex-start" class="!flex-nowrap !gap-x-10 h-[45px]">
    <n-flex class="path-part !gap-x-6 h-full !flex-nowrap" align="center">
      <n-button text :disabled="disabledBack" @click="handlePathBack">
        <n-icon size="16" class="icon-hover" :component="ArrowBackIosFilled" />
      </n-button>

      <n-button text :disabled="disabledForward" @click="handlePathForward">
        <n-icon :component="ArrowForwardIosFilled" size="16" class="icon-hover" />
      </n-button>
    </n-flex>
    <n-flex class="file-part flex-[5] h-full">
      <n-flex
        v-for="item of filePathList"
        :key="item.id"
        align="center"
        justify="center"
        class="file-node !flex-nowrap h-full"
      >
        <n-icon :component="Folder" size="18" :color="item.active ? '#63e2b7' : ''" />
        <n-text depth="1" class="text-[16px] cursor-pointer" :strong="item.active">
          {{ item.path }}
        </n-text>
        <n-icon v-if="item.showArrow" :component="ArrowForwardIosFilled" size="16" />
      </n-flex>
    </n-flex>
    <n-flex class="upload-part" align="center" justify="center">
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
      placement="bottom-start"
      trigger="manual"
      :x="x"
      :y="y"
      :options="options"
      :show="showDropdown"
      :on-clickoutside="onClickoutside"
      @select="handleSelect"
    />
  </n-flex>
</template>

<script setup lang="ts">
import { Folder } from '@vicons/tabler';
import type { DataTableColumns, DropdownOption, UploadFileInfo } from 'naive-ui';
import { NButton, NFlex, NIcon, NText, useMessage } from 'naive-ui';
import { ArrowBackIosFilled, ArrowForwardIosFilled } from '@vicons/material';
import { h, nextTick, ref, watch } from 'vue';
import { useFileManageStore } from '@/store/modules/fileManage.ts';

import type { RowData } from '@/components/pamFileList/index.vue';
import mittBus from '@/utils/mittBus.ts';
import { getFileName } from '@/utils';

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

const message = useMessage();
const fileManageStore = useFileManageStore();

const x = ref(0);
const y = ref(0);
const forwardPath = ref('');
const disabledBack = ref(true);
const showDropdown = ref(false);
const disabledForward = ref(true);
const isShowUploadList = ref(false);

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

const options: DropdownOption[] = [
  {
    label: '编辑',
    key: 'edit'
  },
  {
    label: () => h('span', { style: { color: 'red' } }, '删除'),
    key: 'delete'
  }
];

const onClickoutside = () => {
  showDropdown.value = false;
};

const handleSelect = () => {
  showDropdown.value = false;
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

  mittBus.emit('change-path', { path: backPath });

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

      mittBus.emit('change-path', { path: newForwardPath });
    }
  }
};

// todo)) 子目录下存在 _ 返回的文件目录
const rowProps = (row: RowData) => {
  return {
    style: 'cursor: pointer',
    onContextmenu: (e: MouseEvent) => {
      message.info(JSON.stringify(row, null, 2));

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

      mittBus.emit('change-path', { path: splicePath });
      disabledBack.value = false;
    }
  };
};
</script>
