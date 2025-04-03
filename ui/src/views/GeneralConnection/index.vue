<template>
  <Terminal :socket-instance="socketInstance" :lunaId="lunaId" :origin="origin" />

  <Setting
    v-if="showSettings"
    :settings="settingsConfig"
    @update:open="showSettings = $event"
    class="transition-all duration-500 ease-in-out"
  />
</template>

<script setup lang="ts">
import Setting from '@/components/Setting/index.vue';
import Terminal from '@/components/Terminal/index.vue';

import { useI18n } from 'vue-i18n';
import { onMounted, provide, ref } from 'vue';
import { Palette } from 'lucide-vue-next'
import { useWebSocketManager } from '@/hooks/useWebSocketManager';
import { sendEventToLuna } from '@/components/TerminalComponent/helper';

import type { ISettingConfig } from '@/types';

enum WindowMessageType {
  PING = 'PING',
  PONG = 'PONG',
  CMD = 'CMD',
  FOCUS = 'FOCUS',
  OPEN = 'OPEN',
  FILE = 'FILE'
}

provide('getCurrentThemeName', (themeName: string) => {
  currentThemeName.value = themeName;
})

const { t } = useI18n();
const { createSocket } = useWebSocketManager();

const lunaId = ref<string>('');
const origin = ref<string>('');
const currentThemeName = ref('');
const showSettings = ref<boolean>(false);
const socketInstance = ref<WebSocket | ''>();

const settingsConfig: ISettingConfig = {
  drawerTitle: t('Settings'),
  items: [
    {
      type: 'select',
      label: t('Theme') + ':',
      labelIcon: Palette,
      labelStyle: {
        fontSize: '14px'
      },
      value: 'default'
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
        showSettings.value = true;
        break;
      }
    }
  });
};

socketInstance.value = createSocket();

onMounted(() => {
  initializeWindowEvent();
});
</script>
