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
              <n-text> {{ t('CurrentUser') }}: </n-text>
              <n-text depth="1" strong class="text-sm">{{ userFilters.currentUser?.user }}</n-text>
            </n-flex>

            <n-divider dashed class="!my-2" />

            <n-collapse @item-header-click="handleItemHeaderClick" :default-expanded-names="'online-user'">
              <template #header-extra>
                <ChevronLeft v-if="showLeftArrow" :size="18" class="focus:outline-none" />
                <ChevronDown v-else :size="18" class="focus:outline-none" />
              </template>
              <n-collapse-item :title="t('OnlineUser') + ':'" name="online-user">
                <n-flex
                  v-if="userFilters.otherUsers.length > 0"
                  v-for="item in userFilters.otherUsers"
                  :key="item.user_id"
                  align="center"
                  justify="space-between"
                  class="w-full"
                >
                  <n-tag closable size="small" type="primary" :bordered="false" @close="handlePositiveClick(item)">
                    <span class="text-xs">{{ item.user }}</span>
                  </n-tag>
                </n-flex>
                <n-empty v-else :description="t('NoOnlineUser')" />
              </n-collapse-item>
            </n-collapse>
          </n-flex>
        </n-card>
      </template>

      <template v-if="item.type === 'create'">
        <n-card size="small">
          <Share
            :share-id="currentTerminalConn.shareId"
            :share-code="currentTerminalConn.shareCode"
            :share-enable="currentTerminalConn.enableShare"
            :user-options="currentTerminalConn.userOptions"
            @create-share-url="handleCreateShareUrl"
            @search-share-user="handleSearchShareUser"
          />
        </n-card>
      </template>

      <template v-if="item.type === 'keyboard'">
        <Keyboard @write-command="handleWriteCommand" />
      </template>
    </n-form-item>
  </div>
</template>

<script setup lang="ts">
import xtermTheme from 'xterm-theme';
import Share from '@/components/Drawer/components/Share/index.vue';
import Keyboard from '@/components/Drawer/components/Keyboard/index.vue';

import { useI18n } from 'vue-i18n';
import { storeToRefs } from 'pinia';
import { formatMessage } from '@/utils';
import { readText } from 'clipboard-polyfill';
import { FORMATTER_MESSAGE_TYPE } from '@/types/modules/message.type';
import { ref, watch, computed, nextTick } from 'vue';
import { useConnectionStore } from '@/store/modules/useConnection';
import { Ellipsis, ChevronLeft, ChevronDown } from 'lucide-vue-next';
import { useTerminalSettingsStore } from '@/store/modules/terminalSettings';

import type { OnlineUser } from '@/types/modules/user.type';
import type { SettingConfig } from '@/types/modules/setting.type';

defineProps<{
  settings: SettingConfig;
}>();

const terminalSettingsStore = useTerminalSettingsStore();
const connectionStore = useConnectionStore();

const { t } = useI18n();
const { theme } = storeToRefs(terminalSettingsStore);

const userFilters = computed(() => {
  const users = currentTerminalConn.value.onlineUsers;
  return {
    currentUser: users.find(item => item.primary),
    otherUsers: users.filter(item => !item.primary)
  };
});

const currentTerminalConn = computed(() => {
  // TODO 默认取 map 中第 0 项
  const conn = Array.from(connectionStore.connectionStateMap.values())[0] || {};

  return {
    socket: conn.socket,
    terminal: conn.terminal,
    shareId: conn.shareId || '',
    shareCode: conn.shareCode || '',
    sessionId: conn.sessionId || '',
    terminalId: conn.terminalId || '',
    enableShare: conn.enableShare || false,
    userOptions: conn.userOptions || [],
    onlineUsers: conn.onlineUsers || []
  };
});

const origin = computed(() => window.location.origin);

const showLeftArrow = ref(false);
const currentTheme = ref(theme?.value);
const themeOptions = ref([
  { label: 'Default', value: 'Default' },
  ...Object.keys(xtermTheme).map(item => ({ label: item, value: item }))
]);

watch(
  () => theme?.value,
  value => {
    currentTheme.value = value;
  }
);

watch(
  () => currentTerminalConn.value.onlineUsers,
  value => {
    showLeftArrow.value = value.length > 1;
  }
);

/**
 * @description 更新主题
 * @param value
 */
const handleUpdateTheme = (value: string) => {
  currentTheme.value = value;
  terminalSettingsStore.setDefaultTerminalConfig('theme', value);

  nextTick(() => {
    const { socket, terminalId } = currentTerminalConn.value;
    socket?.send(
      formatMessage(
        terminalId,
        'TERMINAL_SYNC_USER_PREFERENCE',
        JSON.stringify({
          terminal_theme_name: value
        })
      )
    );
  });
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
  showLeftArrow.value = !data.expanded;
};

/**
 * @description 移除在线用户
 * @param userMeta
 */
const handlePositiveClick = (userMeta: OnlineUser) => {
  const { socket, terminalId, sessionId } = currentTerminalConn.value;
  socket?.send(
    formatMessage(
      terminalId,
      FORMATTER_MESSAGE_TYPE.TERMINAL_SHARE_USER_REMOVE,
      JSON.stringify({
        session: sessionId,
        user_meta: userMeta
      })
    )
  );
};

/**
 * @description 创建分享链接
 * @param shareLinkRequest
 */
const handleCreateShareUrl = (shareLinkRequest: any) => {
  const { socket, terminalId, sessionId } = currentTerminalConn.value;
  socket?.send(
    formatMessage(
      terminalId,
      FORMATTER_MESSAGE_TYPE.TERMINAL_SHARE,
      JSON.stringify({
        origin: origin.value,
        session: sessionId,
        users: shareLinkRequest.users,
        expired_time: shareLinkRequest.expiredTime,
        action_permission: shareLinkRequest.actionPerm
      })
    )
  );
};

/**
 * @description 搜索分享用户
 * @param query
 */
const handleSearchShareUser = (query: string) => {
  const { socket, terminalId } = currentTerminalConn.value;
  socket?.send(formatMessage(terminalId, FORMATTER_MESSAGE_TYPE.TERMINAL_GET_SHARE_USER, JSON.stringify({ query })));
};

/**
 * @description 写入命令
 * @param command
 */
const handleWriteCommand = async (command: string) => {
  const { terminal } = currentTerminalConn.value;

  switch (command) {
    case 'Stop':
      terminal?.paste('\x03');
      break;
    case 'Save':
      terminal?.paste('\x13');
      break;
    case 'Undo':
      terminal?.paste('\x1A');
      break;
    case 'Paste':
      terminal?.paste(await readText());
      break;
    case 'ArrowUp':
      terminal?.paste('\x1b[A');
      break;
    case 'ArrowDown':
      terminal?.paste('\x1b[B');
      break;
    case 'ArrowLeft':
      terminal?.paste('\x1b[D');
      break;
    case 'ArrowRight':
      terminal?.paste('\x1b[C');
      break;
  }
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
