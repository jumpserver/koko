<script setup lang="ts">
import { Upload as UploadIcon } from 'lucide-vue-next';

import type { UploadFileInfo } from 'naive-ui';

interface Props {
  t: (key: string) => string;
}

const props = defineProps<Props>();
const emit = defineEmits(['file-change']);

const handleFileChange = (options: { fileList: UploadFileInfo }) => {
  emit('file-change', options);
};
</script>

<template>
  <n-upload directory-dnd action="#" :multiple="false" :default-upload="false" @change="handleFileChange">
    <n-upload-dragger>
      <div style="margin-bottom: 12px">
        <UploadIcon :size="48" />
      </div>
      <n-text style="font-size: 16px">
        {{ t('UploadTips') }}
      </n-text>
    </n-upload-dragger>
  </n-upload>
</template>
