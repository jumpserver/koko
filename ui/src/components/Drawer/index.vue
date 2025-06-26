<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { computed, reactive, ref } from 'vue';
import { FolderKanban, Keyboard as KeyboardIcon, Palette, Share2, UsersRound } from 'lucide-vue-next';

import type { SettingConfig } from '@/types/modules/setting.type';
import type { ContentType } from '@/types/modules/connection.type';

import { FILE_SUFFIX_DATABASE } from '@/utils/config';

import Setting from './components/Setting/index.vue';
import Keyboard from './components/Keyboard/index.vue';
import SessionShare from './components/SessionShare/index.vue';
import FileManager from './components/FileManagement/index.vue';

const props = defineProps<{
  // title: string;

  showDrawer: boolean;

  // token: string;

  // contentType: ContentType;

  // defaultProtocol: string;
}>();

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void;
  (e: 'update:content-type', value: ContentType): void;
}>();

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

const { t } = useI18n();

const drawerMinWidth = ref(350);
const drawerMaxWidth = ref(1024);
const settingsConfig = reactive<SettingConfig>({
  drawerTitle: t('Settings'),
  items: [
    {
      type: 'select',
      label: `${t('Theme')}:`,
      labelIcon: Palette,
      labelStyle: {
        fontSize: '14px',
      },
      showMore: false,
      value: 'default',
    },
    {
      type: 'list',
      label: `${t('OnlineUsers')}:`,
      labelIcon: UsersRound,
      labelStyle: {
        fontSize: '14px',
      },
    },
    {
      type: 'create',
      label: `${t('CreateLink')}:`,
      labelIcon: Share2,
      labelStyle: {
        fontSize: '14px',
      },
      showMore: false,
    },
    {
      type: 'keyboard',
      label: `${t('Hotkeys')}:`,
      labelIcon: Keyboard,
      labelStyle: {
        fontSize: '14px',
      },
    },
  ],
});

const drawerDefaultWidth = computed(() => {
  return 502;
});
const disabledFileManager = computed(() => {
  return FILE_SUFFIX_DATABASE.includes(props.defaultProtocol);
});

/**
 * @description 关闭抽屉
 */

function handleChangeTab(tab: ContentType) {
  emit('update:content-type', tab);
}
</script>

<template>
  <n-drawer
    id="drawer-inner-target"
    resizable
    placement="right"
    :show="showDrawer"
    :show-mask="false"
    :default-width="600"
  >
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
