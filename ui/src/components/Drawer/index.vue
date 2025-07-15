<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import { FolderKanban, Keyboard as KeyboardIcon, Share2, X } from 'lucide-vue-next';

import type { LunaMessage } from '@/types/modules/postmessage.type';

import mittBus from '@/utils/mittBus';
import { lunaCommunicator } from '@/utils/lunaBus';
import { LUNA_MESSAGE_TYPE } from '@/types/modules/message.type';
import { useConnectionStore } from '@/store/modules/useConnection';

import Keyboard from './components/Keyboard/index.vue';
import SessionShare from './components/SessionShare/index.vue';
import FileManager from './components/FileManagement/index.vue';

const props = defineProps<{
  hiddenFileManager?: boolean;
}>();

// 15s 最大等待时间
const MAX_WAIT_TIME = 1000 * 15;
const DISABLED_PROTOCOLS = ['database', 'device'];

const { t } = useI18n();
const connectionStore = useConnectionStore();

const drawerTabs = [
  {
    name: 'hotkeys',
    label: t('Hotkeys'),
    icon: KeyboardIcon,
    component: Keyboard,
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
];

const hasToken = ref(false);
const showEmpty = ref(false);
const drawerStatus = ref(false);
const isRequestingToken = ref(false);
const fileManagerToken = ref('');
const timeoutId = ref<number | null>(null);

watch(
  () => hasToken.value,
  newVal => {
    if (newVal) {
      // 为 true 则获取到 token
      isRequestingToken.value = false;
      showEmpty.value = false;

      if (timeoutId.value) {
        clearTimeout(timeoutId.value);
        timeoutId.value = null;
      }
    }
  }
);

const filteredDrawerTabs = computed(() => {
  if (props.hiddenFileManager || DISABLED_PROTOCOLS.includes(lunaCommunicator.getProtocol())) {
    return drawerTabs.filter(tab => tab.name !== 'file-manager' && tab.name !== 'session-detail');
  }

  return drawerTabs;
});

const closeDrawer = () => {
  drawerStatus.value = false;
};

const handleOpenDrawer = () => {
  if (!drawerStatus.value) {
    drawerStatus.value = true;
  }
};

const handleTabChange = (tabName: string) => {
  if (tabName === 'file-manager' && !hasToken.value && !isRequestingToken.value) {
    if (timeoutId.value) {
      clearTimeout(timeoutId.value);
      timeoutId.value = null;
    }

    isRequestingToken.value = true;
    showEmpty.value = false;
    lunaCommunicator.sendLuna(LUNA_MESSAGE_TYPE.CREATE_FILE_CONNECT_TOKEN, '');

    timeoutId.value = setTimeout(() => {
      if (!hasToken.value && isRequestingToken.value) {
        showEmpty.value = true;
        isRequestingToken.value = false;
      }
    }, MAX_WAIT_TIME);
  }
};

const handleCreateFileConnectToken = (message: LunaMessage) => {
  const token = (message as any).token;

  if (token) {
    fileManagerToken.value = token;
    hasToken.value = true;
  } else {
    if (isRequestingToken.value) {
      showEmpty.value = true;
    }
    isRequestingToken.value = false;
  }
};

const handleReconnect = () => {
  if (timeoutId.value) {
    clearTimeout(timeoutId.value);
    timeoutId.value = null;
  }

  hasToken.value = false;
  showEmpty.value = false;
  fileManagerToken.value = '';
  isRequestingToken.value = true;

  lunaCommunicator.sendLuna(LUNA_MESSAGE_TYPE.CREATE_FILE_CONNECT_TOKEN, '');

  timeoutId.value = setTimeout(() => {
    if (!hasToken.value && isRequestingToken.value) {
      showEmpty.value = true;
      isRequestingToken.value = false;
    }
  }, MAX_WAIT_TIME);
};

onMounted(() => {
  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.OPEN, handleOpenDrawer);
  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.GET_FILE_CONNECT_TOKEN, handleCreateFileConnectToken);

  mittBus.on('open-setting', () => {
    drawerStatus.value = !drawerStatus.value;
  });
});

onUnmounted(() => {
  if (timeoutId.value) {
    clearTimeout(timeoutId.value);
    timeoutId.value = null;
  }

  lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.OPEN, handleOpenDrawer);
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
    :default-width="502"
    :min-width="502"
    :max-width="800"
    class="relative"
    :style="{
      display: drawerStatus ? 'block' : 'none',
      opacity: drawerStatus ? 1 : 0,
    }"
  >
    <n-drawer-content :native-scrollbar="false">
      <template #header>
        <n-flex align="center" justify="space-between">
          <n-text depth="1">
            {{ connectionStore.assetName }}
          </n-text>
          <X class="cursor-pointer" :size="18" @click="closeDrawer" />
        </n-flex>
      </template>

      <n-tabs
        size="medium"
        type="line"
        justify-content="space-evenly"
        :default-value="filteredDrawerTabs[0].name"
        class="custom-tabs"
        @update:value="handleTabChange"
      >
        <n-tab-pane v-for="tab in filteredDrawerTabs" :key="tab.name" display-directive="show" :name="tab.name">
          <template #tab>
            <n-flex align="center">
              <component :is="tab.icon" :size="16" />
              <span>{{ tab.label }}</span>
            </n-flex>
          </template>

          <component
            :is="tab.component"
            :sftp-token="fileManagerToken"
            :show-empty="showEmpty"
            @reconnect="handleReconnect"
          />
        </n-tab-pane>
      </n-tabs>
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
