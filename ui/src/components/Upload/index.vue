<script setup lang="ts">
import type { UploadFileInfo } from 'naive-ui';

import { ArchiveOutline as ArchiveIcon } from '@vicons/ionicons5';

defineProps<{
  t: any;
  fileList: UploadFileInfo[];
  fileListLength: number;
}>();

const emit = defineEmits<{
  'update:fileList': [fileList: UploadFileInfo[]];
  'update:fileListLength': [length: number];
}>();

function handleFileChange(options: { fileList: UploadFileInfo[] }) {
  emit('update:fileList', options.fileList);
  emit('update:fileListLength', options.fileList.length);
}
</script>

<template>
  <n-upload directory-dnd action="#" :default-upload="false" :max="1" @change="handleFileChange">
    <n-upload-dragger>
      <div style="margin-bottom: 12px">
        <n-icon size="48" :depth="3">
          <ArchiveIcon />
        </n-icon>
      </div>
      <n-text style="font-size: 16px">
        {{ t('UploadTips') }}
      </n-text>
    </n-upload-dragger>
  </n-upload>
</template>

<style scoped lang="scss">
:deep(.n-upload-file-list) {
  margin: 10px 0 10px 0;
}
</style>
