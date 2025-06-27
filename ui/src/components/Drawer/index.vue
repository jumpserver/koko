<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { inject, onMounted, onUnmounted } from 'vue';
import { FolderKanban, Keyboard as KeyboardIcon, Share2 } from 'lucide-vue-next';

import type { SettingConfig } from '@/types/modules/setting.type';
import type { ContentType } from '@/types/modules/connection.type';

import { lunaCommunicator } from '@/utils/lunaBus';
import { FILE_SUFFIX_DATABASE } from '@/utils/config';
import { LUNA_MESSAGE_TYPE } from '@/types/modules/message.type';

import Keyboard from './components/Keyboard/index.vue';
import SessionShare from './components/SessionShare/index.vue';
import FileManager from './components/FileManagement/index.vue';

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void;
  (e: 'update:content-type', value: ContentType): void;
}>();

const manualSetTheme = inject<(theme: string) => void>('manual-set-theme');

const DRAWER_HEADER_STYLE = {
  display: 'none',
  height: '55px',
  color: '#EBEBEB',
  fontSize: '16px',
  fontWeight: '500',
  fontFamily: 'PingFang SC',
};

const drawerTabs = [
  {
    name: 'file-manager',
    label: '文件管理',
    icon: FolderKanban,
  },
  {
    name: 'share-session',
    label: '会话分享',
    icon: Share2,
    component: SessionShare,
  },
  {
    name: 'hotkeys',
    label: '快捷键',
    icon: KeyboardIcon,
    component: Keyboard,
  },
];

const handleMainThemeChange = (themeName: any) => {
  manualSetTheme?.(themeName!.data as string);
};

// const { t } = useI18n();

onMounted(() => {
  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.CHANGE_MAIN_THEME, handleMainThemeChange);
});

onUnmounted(() => {
  lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.CHANGE_MAIN_THEME, handleMainThemeChange);
});

/**
 * @description 关闭抽屉
 */

function handleChangeTab(tab: ContentType) {
  emit('update:content-type', tab);
}
</script>

<template>
  <n-drawer id="drawer-inner-target" resizable placement="right" :show="true" :show-mask="false" :default-width="600">
    <n-drawer-content closable :native-scrollbar="false" :header-style="DRAWER_HEADER_STYLE">
      <n-tabs size="small" type="segment" :pane-style="{ marginTop: '10px' }" @update:value="handleChangeTab">
        <n-tab-pane v-for="tab in drawerTabs" :key="tab.name" :name="tab.name">
          <template #tab>
            <n-flex align="center">
              <component :is="tab.icon" :size="16" />
              <span>{{ tab.label }}</span>
            </n-flex>
          </template>

          <component :is="tab.component" />
        </n-tab-pane>
      </n-tabs>
    </n-drawer-content>
  </n-drawer>
</template>
