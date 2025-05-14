<template>
  <n-drawer
    id="drawer-inner-target"
    resizable
    placement="right"
    :show="showDrawer"
    :min-width="drawerMinWidth"
    :max-width="drawerMaxWidth"
    :width="drawerDefaultWidth"
    @mask-click="closeDrawer"
  >
    <n-drawer-content closable :native-scrollbar="false" :header-style="DRAWER_HEADER_STYLE">
      <n-tabs
        size="small"
        type="bar"
        :value="contentType"
        :pane-style="{ marginTop: '10px' }"
        @update:value="handleChangeTab"
      >
        <n-tab-pane name="setting" display-directive="if" :tab="t('Settings')">
          <Setting :settings="settingsConfig" />
        </n-tab-pane>
        <n-tab-pane
          name="file-manager"
          display-directive="if"
          :disabled="disabledFileManager"
          :tab="t('FileManagement')"
        >
          <FileManager :sftp-token="token" />
        </n-tab-pane>
      </n-tabs>
    </n-drawer-content>
  </n-drawer>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { reactive, ref, computed } from 'vue';
import { FILE_SUFFIX_DATABASE } from '@/config';
import { Palette, Share2, UsersRound, Keyboard } from 'lucide-vue-next';

import Setting from './components/Setting/index.vue';
import FileManager from './components/FileManagement/index.vue';

import type { SettingConfig } from '@/types/modules/setting.type';
import type { ContentType } from '@/types/modules/connection.type';

const DRAWER_HEADER_STYLE = {
  display: 'none',
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

  defaultProtocol: string;
}>();

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void;
  (e: 'update:content-type', value: ContentType): void;
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
const disabledFileManager = computed(() => {
  return FILE_SUFFIX_DATABASE.includes(props.defaultProtocol);
});

/**
 * @description 关闭抽屉
 */
const closeDrawer = () => {
  emit('update:open', false);
};

const handleChangeTab = (tab: ContentType) => {
  emit('update:content-type', tab);
};
</script>
