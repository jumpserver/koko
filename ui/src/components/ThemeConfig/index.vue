<template>
  <teleport to="body">
    <n-dialog
      v-if="showThemeConfig"
      :title="t('Theme')"
      :show-icon="false"
      class="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2"
      @on-close="handleClose"
    >
      <template #default>
        <n-form label-placement="top" size="medium">
          <n-grid :cols="24" :x-gap="24">
            <n-form-item-gi :span="24">
              <n-select
                v-model:value="theme"
                :placeholder="t('SelectTheme')"
                :options="themes"
                @update:value="setTheme"
                class="pr-[35px]"
              />
              <n-button :loading="loading">同步</n-button>
            </n-form-item-gi>
            <n-form-item-gi></n-form-item-gi>
            <n-form-item-gi></n-form-item-gi>
          </n-grid>
        </n-form>
      </template>
    </n-dialog>
  </teleport>
</template>

<script setup lang="ts">
import mittBus from '@/utils/mittBus.ts';

import { useI18n } from 'vue-i18n';
import { computed, onMounted, onUnmounted, ref } from 'vue';
import { useMessage } from 'naive-ui';

import xtermTheme from 'xterm-theme';

const { t } = useI18n();

const message = useMessage();

const theme = ref<string>('Default');

const loading = ref<boolean>(false);
const showThemeConfig = ref<boolean>(false);

mittBus.on('show-theme-config', () => {
  showThemeConfig.value = !showThemeConfig.value;
});

const themes = computed(() => {
  return [
    {
      label: 'Default',
      value: 'Default'
    },
    ...Object.keys(xtermTheme).map(item => {
      return {
        label: item,
        value: item
      };
    })
  ];
});

const setTheme = (value: string) => {
  mittBus.emit('set-theme', { themeName: value });
};

const handleClose = () => {
  showThemeConfig.value = false;
};

onMounted(() => {
  console.log('xtermTheme', xtermTheme);
});

onUnmounted(() => {
  mittBus.off('show-theme-config');
});
</script>

<style scoped lang="scss"></style>
