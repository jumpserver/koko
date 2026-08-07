<script setup lang="ts">
import type { UploadFileInfo } from 'naive-ui';

import { Upload as UploadIcon } from 'lucide-vue-next';

interface Props {
  t: (key: string) => string;
}

defineProps<Props>();
const emit = defineEmits(['fileChange']);

const handleFileChange = (options: { fileList: UploadFileInfo[] }) => {
  emit('fileChange', options);
};
</script>

<template>
  <n-upload action="#" :multiple="false" :max="1" :default-upload="false" @change="handleFileChange">
    <n-upload-dragger>
      <n-flex justify="center" align="center" class="mb-3">
        <UploadIcon :size="48" />
      </n-flex>
      <n-text style="font-size: 16px">
        {{ t('UploadTips') }}
      </n-text>
    </n-upload-dragger>
  </n-upload>
</template>
