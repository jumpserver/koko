<template>
  <div v-for="item of settings.items" :key="item.label">
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
          size="small"
          :value="currentTheme"
          :options="themeOptions"
          @keydown="previewTheme"
          @update:value="handleUpdateTheme"
        />
      </template>

      <template v-if="item.type === 'list'">
        <n-card size="small">
          <n-flex justify="center" vertical class="w-full">
            <n-flex align="center">
              <n-text> 当前用户: </n-text>
              <n-text depth="1" strong class="text-sm">{{ currentUser.user }}</n-text>
            </n-flex>

            <n-divider dashed class="!my-2" />

            <n-collapse @item-header-click="handleItemHeaderClick" :default-expanded-names="'online-user'">
              <template #header-extra>
                <ChevronLeft v-if="showLeftArrow" :size="18" class="focus:outline-none" />
                <ChevronDown v-else :size="18" class="focus:outline-none" />
              </template>
              <n-collapse-item title="在线用户:" name="online-user">
                <n-flex
                  v-if="onlineUsers.length > 0"
                  v-for="item in onlineUsers"
                  :key="item.user_id"
                  align="center"
                  justify="space-between"
                  class="w-full"
                >
                  <n-tag closable size="small" type="primary" :bordered="false" @close="handlePositiveClick(item)">
                    <span class="text-xs">{{ item.user }}</span>
                  </n-tag>
                </n-flex>
                <n-empty v-else description="暂无在线用户" />
              </n-collapse-item>
            </n-collapse>
          </n-flex>
        </n-card>
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

      <template v-if="item.type === 'keyboard'">
        <Keyboard />
      </template>
    </n-form-item>
  </div>
</template>

<script setup lang="ts">
import xtermTheme from 'xterm-theme';
import mittBus from '@/utils/mittBus.ts';
import Keyboard from '../Keyboard/index.vue';
import Share from '@/components/Share/index.vue';

import { useI18n } from 'vue-i18n';
import { ref, watch, computed, onMounted } from 'vue';
import { useTerminalSettingsStore } from '@/store/modules/terminalSettings';
import { useConnectionStore } from '@/store/modules/useConnection';
import { Ellipsis, ChevronLeft, ChevronDown } from 'lucide-vue-next';
import { storeToRefs } from 'pinia';

import type { SettingConfig } from '@/types/modules/setting.type';
import type { ShareUserOptions, OnlineUser } from '@/types/modules/user.type';

const props = defineProps<{
  settings: SettingConfig;
}>();

const { t } = useI18n();
const terminalSettingsStore = useTerminalSettingsStore();
const connectionStore = useConnectionStore();

// 获取当前终端ID
const currentTerminalId = computed(() => {
  return terminalSettingsStore.activeTerminalId;
});

// 从connectionStore获取连接状态
const connectionState = computed(() => {
  return connectionStore.getConnectionState(currentTerminalId.value) || {};
});

const shareId = computed(() => connectionState.value.shareId || '');
const shareCode = computed(() => connectionState.value.shareCode || '');
const shareEnable = computed(() => connectionState.value.enableShare || false);
const currentOnlineUsers = computed(() => connectionState.value.onlineUsers || []);
const shareUserOptions = computed(() => connectionState.value.userOptions || []);

const showLeftArrow = ref(false);
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

const currentUser = computed(() => {
  return currentOnlineUsers.value.filter(item => item.primary)[0];
});

const onlineUsers = computed(() => {
  return currentOnlineUsers.value.filter(item => !item.primary);
});

watch(
  () => currentOnlineUsers.value,
  value => {
    if (value.length === 1) {
      return (showLeftArrow.value = false);
    }

    showLeftArrow.value = true;
  }
);

watch(
  () => terminalSettingsStore.theme,
  value => {
    currentTheme.value = value;
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

/**
 * @description 点击折叠按钮
 * @param data
 */
const handleItemHeaderClick = (data: { name: string | number; expanded: boolean; event: MouseEvent }) => {
  data.expanded ? (showLeftArrow.value = false) : (showLeftArrow.value = true);
};

/**
 * @description 移除在线用户
 * @param user_id
 */
const handlePositiveClick = (userMeta: OnlineUser) => {
  mittBus.emit('remove-share-user', {
    // todo 参数没有必要
    sessionId: '',
    userMeta: userMeta,
    type: 'TERMINAL_SHARE_USER_REMOVE'
  });
};
</script>

<style scoped lang="scss">
.n-form-item.n-form-item--top-labelled .n-form-item-label {
  align-items: center;
  padding: unset;
}

.n-collapse {
  :deep(.n-collapse-item-arrow) {
    display: none !important;
  }

  :deep(.n-collapse-item__content-inner) {
    padding-top: 5px !important;
  }
}

:deep(.n-form-item-label__text) {
  width: 100%;
}
</style>
