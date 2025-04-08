<template>
  <Terminal
    :socket-instance="socketInstance"
    :lunaId="lunaId"
    :origin="origin"
    @update:onlineUsers="handleUpdateOnlineUsers"
    @update:shareResult="handleUpdateShareResult"
    @update:shareEnable="handleUpdateShareEnable"
    @update:shareUserOptions="handleUpdateShareUserOptions"
  />

  <Setting
    v-if="showDrawer"
    :settings="settingsConfig"
    :share-id="currentShareId"
    :share-code="currentShareCode"
    :share-enable="currentEnableShare"
    :socket-instance="socketInstance"
    :share-user-options="currentUserOptions"
    :current-online-users="currentOnlineUsers"
    @update:open="showDrawer = $event"
    class="transition-all duration-500 ease-in-out"
  />
</template>

<script setup lang="ts">
import Setting from '@/components/Setting/index.vue';
import Terminal from '@/components/Terminal/index.vue';

import { useI18n } from 'vue-i18n';
import { onMounted, ref } from 'vue';
import { Palette, Share2, UsersRound } from 'lucide-vue-next';
import { useWebSocketManager } from '@/hooks/useWebSocketManager';
import { sendEventToLuna } from '@/components/TerminalComponent/helper';

import type { SettingConfig } from '@/types/modules/setting.type';
import type { ShareUserOptions } from '@/types/modules/user.type';

enum WindowMessageType {
  PING = 'PING',
  PONG = 'PONG',
  CMD = 'CMD',
  FOCUS = 'FOCUS',
  OPEN = 'OPEN',
  FILE = 'FILE'
}

const { t } = useI18n();
const { createSocket }: { createSocket: () => WebSocket | '' } = useWebSocketManager();

const lunaId = ref<string>('');
const origin = ref<string>('');
const currentShareId = ref<string>('');
const currentShareCode = ref<string>('');
const showDrawer = ref<boolean>(false);
const currentEnableShare = ref<boolean>(false);
const currentOnlineUsers = ref<any>([]);
const currentUserOptions = ref<ShareUserOptions[]>([]);
const socketInstance = ref<WebSocket | ''>('');

const settingsConfig: SettingConfig = {
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
      label: t('OnlineUsers'),
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
    }
  ]
};

const initializeWindowEvent = () => {
  window.addEventListener('message', (e: MessageEvent) => {
    const windowMessage = e.data;

    switch (windowMessage.name) {
      case WindowMessageType.PING: {
        lunaId.value = windowMessage.id;
        origin.value = e.origin;

        sendEventToLuna(WindowMessageType.PONG, '', lunaId.value, origin.value);
        break;
      }
      case WindowMessageType.OPEN: {
        // 默认情况打开的就是 Settings
        showDrawer.value = true;
        break;
      }
    }
  });
};

/**
 * @description 更新分享结果
 * @param param
 */
const handleUpdateShareResult = ({ shareId, shareCode }: { shareId: string; shareCode: string }) => {
  currentShareId.value = shareId;
  currentShareCode.value = shareCode;
};
/**
 * @description 更新分享用户选项
 * @param userOptions
 */
const handleUpdateShareUserOptions = (userOptions: ShareUserOptions[]) => {
  currentUserOptions.value = userOptions;
};
/**
 * @description 更新分享选项
 * @param param
 */
const handleUpdateShareEnable = (shareEnable: boolean) => {
  currentEnableShare.value = shareEnable;
};
/**
 * @description 更新在线用户
 * @param onlineUsers
 */
const handleUpdateOnlineUsers = (onlineUsers: any) => {
  currentOnlineUsers.value = onlineUsers;
};

socketInstance.value = createSocket();

onMounted(() => {
  initializeWindowEvent();
});
</script>
