<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { computed, inject, onMounted, onUnmounted, ref } from 'vue';
import { Focus, FolderKanban, Keyboard as KeyboardIcon, Share2, X } from 'lucide-vue-next';

import type { LunaMessage } from '@/types/modules/postmessage.type';

import mittBus from '@/utils/mittBus';
import { lunaCommunicator } from '@/utils/lunaBus';
import { LUNA_MESSAGE_TYPE } from '@/types/modules/message.type';

import Keyboard from './components/Keyboard/index.vue';
import SessionShare from './components/SessionShare/index.vue';
import FileManager from './components/FileManagement/index.vue';
import SessionDetail from './components/SessionDetail/index.vue';

const props = defineProps<{
  hiddenFileManager?: boolean;
}>();

const manualSetTheme = inject<(theme: string) => void>('manual-set-theme');

const { t } = useI18n();

const drawerTabs = [
  {
    name: 'session-detail',
    label: t('SessionDetail'),
    icon: Focus,
    component: SessionDetail,
  },
  {
    name: 'file-manager',
    label: t('FileManagement'),
    icon: FolderKanban,
    component: FileManager,
  },
  {
    name: 'share-session',
    label: t('SessionShare'),
    icon: Share2,
    component: SessionShare,
  },
  {
    name: 'hotkeys',
    label: t('Hotkeys'),
    icon: KeyboardIcon,
    component: Keyboard,
  },
];

const drawerStatus = ref(false);
const fileManagerToken = ref('');

const filteredDrawerTabs = computed(() => {
  if (props.hiddenFileManager) {
    return drawerTabs.filter(tab => tab.name !== 'file-manager' && tab.name !== 'session-detail');
  }

  return drawerTabs;
});

const handleMainThemeChange = (themeName: any) => {
  manualSetTheme?.(themeName!.data as string);
};

const closeDrawer = () => {
  drawerStatus.value = false;
};

const handleOpenDrawer = () => {
  if (!drawerStatus.value) {
    drawerStatus.value = true;
    lunaCommunicator.sendLuna(LUNA_MESSAGE_TYPE.CREATE_FILE_CONNECT_TOKEN, '');
  }
};

const handleCreateFileConnectToken = (message: LunaMessage) => {
  const token = (message as any).token;

  if (token) {
    fileManagerToken.value = token;
  }
};

onMounted(() => {
  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.OPEN, handleOpenDrawer);
  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.CHANGE_MAIN_THEME, handleMainThemeChange);
  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.GET_FILE_CONNECT_TOKEN, handleCreateFileConnectToken);

  mittBus.on('open-setting', () => {
    drawerStatus.value = !drawerStatus.value;
  });
});

onUnmounted(() => {
  lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.OPEN, handleOpenDrawer);
  lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.CHANGE_MAIN_THEME, handleMainThemeChange);
  lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.GET_FILE_CONNECT_TOKEN, handleCreateFileConnectToken);
});
</script>

<template>
  <n-drawer
    id="drawer-inner-target"
    resizable
    placement="right"
    :show="true"
    :show-mask="false"
    :default-width="600"
    class="relative"
    :style="{
      display: drawerStatus ? 'block' : 'none',
      opacity: drawerStatus ? 1 : 0,
      transform: drawerStatus ? 'translateX(0)' : 'translateX(100%)',
      transition: 'all 0.3s cubic-bezier(0.4, 0, 0.2, 1)',
    }"
  >
    <n-drawer-content closable :native-scrollbar="false" :header-style="{ display: 'none' }">
      <n-tabs size="medium" type="line" :default-value="filteredDrawerTabs[0].name" class="custom-tabs">
        <n-tab-pane v-for="tab in filteredDrawerTabs" :key="tab.name" display-directive="show" :name="tab.name">
          <template #tab>
            <n-flex align="center">
              <component :is="tab.icon" :size="16" />
              <span>{{ tab.label }}</span>
            </n-flex>
          </template>

          <component :is="tab.component" :sftp-token="fileManagerToken" />
        </n-tab-pane>
      </n-tabs>

      <X class="absolute top-[28px] right-[1.25rem] cursor-pointer" :size="18" @click="closeDrawer" />
    </n-drawer-content>
  </n-drawer>
</template>

<style scoped lang="scss">
.custom-tabs {
  ::v-deep(.n-tabs-nav--line-type.n-tabs-nav--top.n-tabs-nav) {
    margin-right: 1.5rem;
  }
}
</style>
