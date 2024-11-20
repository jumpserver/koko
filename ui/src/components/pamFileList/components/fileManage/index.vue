<template>
  <n-flex align="center" justify="flex-start" class="!flex-nowrap !gap-x-10 h-[45px]">
    <n-flex class="path-part !gap-x-6 h-full" align="center">
      <n-icon :component="ArrowBackIosFilled" size="16" class="icon-hover" />
      <n-icon :component="ArrowForwardIosFilled" size="16" class="icon-hover" />
    </n-flex>
    <n-flex class="file-part flex-[5] h-full">
      <n-flex class="root-node !flex-nowrap h-full" align="center" justify="center">
        <n-icon :component="Folder" size="18" />
        <n-text depth="1" class="text-[16px] cursor-pointer">root</n-text>
        <n-icon :component="ArrowForwardIosFilled" size="16" />
      </n-flex>
      <n-flex class="file-node !flex-nowrap h-full" align="center" justify="center">
        <n-icon :component="Folder" size="18" color="#63e2b7" />
        <n-text depth="1" class="text-[16px] cursor-pointer">web</n-text>
        <n-icon :component="ArrowForwardIosFilled" size="16" />
      </n-flex>
      <n-flex class="file-node !flex-nowrap h-full" align="center" justify="center">
        <n-icon :component="Folder" size="18" />
        <n-text depth="1" class="text-[16px] cursor-pointer">new</n-text>
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
      :bordered="false"
      :columns="columns"
      :max-height="1150"
      :data="fileManageStore.fileList"
    />
  </n-flex>
</template>

<script setup lang="ts">
import { Folder } from '@vicons/tabler';
import { NButton, NFlex, NIcon, NText } from 'naive-ui';
import { ArrowBackIosFilled, ArrowForwardIosFilled } from '@vicons/material';

import { ref } from 'vue';
import { useFileManageStore } from '@/store/modules/fileManage.ts';

import type { RowData } from '@/components/pamFileList/index.vue';
import type { DataTableColumns, UploadFileInfo } from 'naive-ui';

const props = withDefaults(
  defineProps<{
    columns: DataTableColumns<RowData>;
  }>(),
  {
    columns: () => []
  }
);

const fileManageStore = useFileManageStore();

const isShowUploadList = ref(false);
const fileList = ref<UploadFileInfo[]>([
  {
    id: 'b',
    name: 'file.doc',
    status: 'finished',
    type: 'text/plain'
  }
]);
</script>

<style scoped lang="scss"></style>
