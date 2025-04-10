<template>
  <n-upload
    directory-dnd
    action="#"
    :default-upload="false"
    @change="handleFileChange"
    :max="1"
  >
    <n-upload-dragger>
      <div style="margin-bottom: 12px">
        <n-icon size="48" :depth="3">
          <ArchiveIcon />
        </n-icon>
      </div>
      <n-text style="font-size: 16px"> {{ t('UploadTips') }} </n-text>
    </n-upload-dragger>
  </n-upload>
</template>

<script setup lang="ts">
import { ArchiveOutline as ArchiveIcon } from '@vicons/ionicons5';
import { UploadFileInfo } from 'naive-ui';
import { Ref } from 'vue';

const props = defineProps<{
  t: any;
  fileList: Ref<UploadFileInfo[]>;
  fileListLengthRef: Ref<number>;
}>();

const handleFileChange = (options: { fileList: UploadFileInfo[] }) => {
  props.fileList.value = options.fileList;
  props.fileListLengthRef.value = options.fileList.length;
};
</script>

<style scoped lang="scss">
:deep(.n-upload-file-list) {
  margin: 10px 0 10px 0;
}
</style>
