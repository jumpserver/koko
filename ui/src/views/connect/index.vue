<template>
  <div class="h-full w-full">
    <Terminal 
      :lunaId="lunaId"
      :origin="origin"
      :socket-instance="socketInstance"
      @update:drawer="handleUpdateDrawer"
    />

    <Drawer 
      :title="title"
      :show-drawer="showDrawer"
      :contentType="contentType"
      @update:open="showDrawer = $event"
    />
  </div>
</template>

<script setup lang="ts">
import Drawer from '@/components/Drawer/index.vue';
import Terminal from '@/components/Terminal/index.vue';

import { useI18n } from 'vue-i18n';
import { ref, onMounted } from 'vue';
import { WINDOW_MESSAGE_TYPE } from '@/enum';
import { useWebSocketManager } from '@/hooks/useWebSocketManager';
import { Palette, Share2, UsersRound, Keyboard } from 'lucide-vue-next';
import { sendEventToLuna } from '@/components/TerminalComponent/helper';

import type { SettingConfig } from '@/types/modules/setting.type';
import type { ShareUserOptions, OnlineUser } from '@/types/modules/user.type';

const { t } = useI18n();
const { createSocket }: { createSocket: () => WebSocket | '' } = useWebSocketManager();

const title = ref('');
const lunaId = ref<string>('');
const origin = ref<string>('');
const currentShareId = ref<string>('');
const currentShareCode = ref<string>('');
const contentType = ref<'setting' | 'file-manager'>('setting');
const showDrawer = ref<boolean>(false);
const currentEnableShare = ref<boolean>(false);
const currentOnlineUsers = ref<OnlineUser[]>([]);
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
};

const handleUpdateDrawer = (show: boolean, _title: string, _contentType: 'setting' | 'file-manager') => {
  title.value = _title;
  showDrawer.value = show;
  contentType.value = _contentType;
};

// const receivePostMessage = (): void => {
//   window.addEventListener('message', (e: MessageEvent) => {
//     const windowMessage = e.data;

//     switch (windowMessage.name) {
//       case WINDOW_MESSAGE_TYPE.PING:
//         lunaId.value = windowMessage.id;
//         origin.value = e.origin;

//         sendEventToLuna(WINDOW_MESSAGE_TYPE.PONG, '', lunaId.value, origin.value);
//         break;
//       case WINDOW_MESSAGE_TYPE.OPEN:
//         title.value = t('Settings');
//         contentType.value = 'setting';

//         showDrawer.value = true;
//         break;
//       case WINDOW_MESSAGE_TYPE.FILE:
//         title.value = t('FileManager');
//         contentType.value = 'file-manager';
//         break;
//     }
//   });
// };

onMounted(() => {
  // receivePostMessage();
});
</script>
