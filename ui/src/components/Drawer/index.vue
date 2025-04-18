<template>
  <n-drawer resizable placement="right" :show="showDrawer" :default-width="502" @close="closeDrawer">
    <n-drawer-content closable :title="title" :native-scrollbar="false" :header-style="DRAWER_HEADER_STYLE">
      <template #header>
        <n-flex align="center">
          <span>{{ title }}</span>
        </n-flex>
      </template>

      <Setting v-if="contentType === 'setting'" :settings="settingsConfig" />
      <!-- <FileManager v-if="contentType === 'file-manager'" /> -->
    </n-drawer-content>
  </n-drawer>
</template>

<script setup lang="ts">
import { reactive } from 'vue';
import { useI18n } from 'vue-i18n';
import { Palette, Share2, UsersRound, Keyboard } from 'lucide-vue-next';

import Setting from './components/Setting/index.vue';

import type { SettingConfig } from '@/types/modules/setting.type';

type ContentType = 'setting' | 'file-manager';

const DRAWER_HEADER_STYLE = {
  height: '55px',
  color: '#EBEBEB',
  fontSize: '16px',
  fontWeight: '500',
  fontFamily: 'PingFang SC'
};

defineProps<{
  title: string;

  showDrawer: boolean;

  contentType: ContentType;
}>();

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void;
}>();

const { t } = useI18n();

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
      showMore: true,
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

/**
 * @description 关闭抽屉
 */
const closeDrawer = () => {
  emit('update:open', false);
};
</script>
