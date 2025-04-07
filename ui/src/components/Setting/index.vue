<template>
  <n-drawer v-model:show="show" resizable :default-width="502">
    <!-- Settings 情况下的抽屉 -->
    <n-drawer-content :title="settings.drawerTitle" :native-scrollbar="false" closable>
      <template v-for="item of settings.items" :key="item.label">
        <n-form-item path="theme" :label-style="item.labelStyle" label-align="top">
          <template #label>
            <n-flex align="center" justify="space-between" class="w-full">
              <n-flex align="center" class="!gap-x-2">
                <component :is="item.labelIcon" size="14" />
                <span class="text-sm">{{ item.label }}</span>
              </n-flex>

              <n-tooltip size="small" v-if="item.showMore">
                <template #trigger>
                  <Ellipsis :size="14" class="cursor-pointer focus:outline-none" />
                </template>

                <span> show more </span>
              </n-tooltip>
            </n-flex>
          </template>
          <template v-if="item.type === 'select'">
            <n-select
              :value="currentTheme"
              :options="themeOptions"
              @keydown="previewTheme"
              @update:value="handleUpdateTheme"
              size="small"
            />
          </template>

          <template v-if="item.type === 'create'">
            <n-card size="small">
              <Share
                :share-id="shareId"
                :share-code="shareCode"
                :share-enable="shareEnable"
                :user-options="shareUserOptions"
              />
            </n-card>
          </template>
        </n-form-item>
      </template>
    </n-drawer-content>

    <!-- FileManager 情况下的抽屉 -->
  </n-drawer>
</template>

<script setup lang="ts">
import xtermTheme from 'xterm-theme';
import Share from '@/components/Share/index.vue';

import { ref, watch } from 'vue';
import { Ellipsis } from 'lucide-vue-next';
import { useTerminalSettingsStore } from '@/store/modules/terminalSettings';

import type { ISettingConfig, shareUser } from '@/types';

const props = defineProps<{
  shareId: string;
  shareCode: string;
  shareEnable: boolean;
  shareUserOptions: shareUser[];
  settings: ISettingConfig;
  socketInstance: WebSocket | '';
}>();

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void;
}>();

const terminalSettingsStore = useTerminalSettingsStore();

const show = ref(true);
const currentTheme = ref(terminalSettingsStore.theme);
const themeOptions = ref([
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
]);

watch(
  () => show.value,
  value => {
    if (!value) {
      emit('update:open', value);
    }
  }
);

/**
 * @description 更新主题
 * @param value
 */
const handleUpdateTheme = (value: string) => {
  currentTheme.value = value;
  terminalSettingsStore.setDefaultTerminalConfig('theme', value);
};
/**
 * @description 预览主题
 * @param event
 */
const previewTheme = (event: KeyboardEvent) => {
  if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
    const currentIndex = themeOptions.value.findIndex(theme => theme.value === currentTheme.value);

    let nextIndex = currentIndex;

    if (event.key === 'ArrowUp') {
      // 如果当前索引为 0，则跳转到最后一个选项，否则向上移动
      nextIndex = currentIndex === 0 ? themeOptions.value.length - 1 : currentIndex - 1;
    } else if (event.key === 'ArrowDown') {
      // 如果当前索引为最后一个，则跳转到第一个选项，否则向下移动
      nextIndex = currentIndex === themeOptions.value.length - 1 ? 0 : currentIndex + 1;
    }

    currentTheme.value = themeOptions.value[nextIndex].value;
    terminalSettingsStore.setDefaultTerminalConfig('theme', themeOptions.value[nextIndex].value);
  }

  if (event.key === 'Enter') {
    handleUpdateTheme(currentTheme.value!);
  }
};
</script>

<style scoped lang="scss">
.n-form-item.n-form-item--top-labelled .n-form-item-label {
  align-items: center;
  padding: unset;
}

:deep(.n-form-item-label__text) {
  width: 100%;
}
</style>
