<template>
  <n-drawer v-model:show="show" resizable :default-width="502">
    <!-- Settings 情况下的抽屉 -->
    <n-drawer-content :title="settings.drawerTitle" :native-scrollbar="false" closable>
      <n-form :model="settingFormModal" size="small" label-placement="top">
        <template v-for="item of settings.items" :key="item.label">
          <n-form-item path="theme" :label-style="item.labelStyle">
            <template #label>
              <n-flex align="center" class="!gap-x-2">
                <component :is="item.labelIcon" size="14" />
                <span class="text-sm">{{ item.label }}</span>
              </n-flex>
            </template>
            <template v-if="item.type === 'select'">
              <n-select v-model:value="settingFormModal.theme" :options="themeOptions" size="small" />
            </template>
          </n-form-item>
        </template>
      </n-form>
    </n-drawer-content>

    <!-- FileManager 情况下的抽屉 -->
  </n-drawer>
</template>

<script setup lang="ts">
import xtermTheme from 'xterm-theme';
import { ref, watch, reactive } from 'vue';

import type { ISettingConfig } from '@/types';

const props = defineProps<{
  settings: ISettingConfig;
}>();

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void;
}>();

const show = ref(true);
const settingFormModal = reactive({
  theme: ''
});

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

watch(show, value => {
  if (!value) {
    emit('update:open', value);
  }
});
</script>
