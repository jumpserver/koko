<template>
  <teleport to="#app">
    <n-dialog
      :title="t('Theme')"
      :show-icon="false"
      negative-text="不确认"
      positive-text="确认"
      class="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2"
      @positive-click="handlePositiveClick"
      @negative-click="handleNegativeClick"
    >
      <template #default> 123 </template>
    </n-dialog>
  </teleport>
</template>

<script setup lang="ts">
import mittBus from '@/utils/mittBus.ts';

import { useI18n } from 'vue-i18n';
import { useMessage } from 'naive-ui';
import { onMounted, ref } from 'vue';

const emit = defineEmits<{
  (e: 'setTheme', value: string): void;
}>();

const { t } = useI18n();

const message = useMessage();

const showThemeConfig = ref<boolean>(false);

mittBus.on('show-theme-config', () => {
  console.log(1);
  showThemeConfig.value = !showThemeConfig.value;
});

onMounted(() => {});

const handleNegativeClick = () => {
  message.warning('取消');
};
const handlePositiveClick = () => {
  message.success('确认');
};
</script>

<style scoped lang="scss"></style>
