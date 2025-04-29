<template>
  <n-drawer
    resizable
    placement="right"
    :show="showDrawer"
    :min-width="drawerMinWidth"
    :max-width="drawerMaxWidth"
    :default-width="drawerDefaultWidth"
    @update:show="closeDrawer"
  >
    <n-drawer-content closable :title="title" :native-scrollbar="false" :header-style="DRAWER_HEADER_STYLE">
      <template #header>
        <n-flex align="center">
          <span>{{ title }}</span>
        </n-flex>
      </template>

      <Setting v-if="contentType === 'setting'" :settings="settingsConfig" />
      <FileManager v-if="contentType === 'file-manager'" :sftp-token="token" />
    </n-drawer-content>
  </n-drawer>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { reactive, ref, computed } from 'vue';
import { Palette, Share2, UsersRound, Keyboard } from 'lucide-vue-next';

import Setting from './components/Setting/index.vue';
import FileManager from './components/FileManagement/index.vue';

import type { SettingConfig } from '@/types/modules/setting.type';
import type { ContentType } from '@/types/modules/connection.type';

const DRAWER_HEADER_STYLE = {
  height: '55px',
  color: '#EBEBEB',
  fontSize: '16px',
  fontWeight: '500',
  fontFamily: 'PingFang SC'
};

const props = defineProps<{
  title: string;

  showDrawer: boolean;

  token?: string;

  contentType: ContentType;
}>();

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void;
}>();

const { t } = useI18n();

const drawerMinWidth = ref(350);
const drawerMaxWidth = ref(1024);
const settingsConfig = reactive<SettingConfig>({
  drawerTitle: t('Settings'),
  items: [
    {
      type: 'select',
      label: t('Theme') + ':',
      labelIcon: Palette,
      labelStyle: {
        fontSize: '14px'
      },
      showMore: false,
      value: 'default'
    },
    {
      type: 'list',
      label: t('OnlineUsers') + ':',
      labelIcon: UsersRound,
      labelStyle: {
        fontSize: '14px'
      }
    },
    {
      type: 'create',
      label: t('CreateLink') + ':',
      labelIcon: Share2,
      labelStyle: {
        fontSize: '14px'
      },
      showMore: false
    },
    {
      type: 'keyboard',
      label: t('Hotkeys') + ':',
      labelIcon: Keyboard,
      labelStyle: {
        fontSize: '14px'
      }
    }
  ]
});

const drawerDefaultWidth = computed(() => {
  return props.contentType === 'setting' ? 502 : 702;
});

/**
 * @description 关闭抽屉
 */
const closeDrawer = () => {
  emit('update:open', false);
};
</script>
