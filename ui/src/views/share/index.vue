<template>
  <n-watermark
    :content="'--'"
    :width="300"
    :height="300"
    :y-offset="60"
    :x-offset="-60"
    :font-size="20"
    :line-height="20"
    :font-family="'Open Sans'"
  >
    <Terminal :socket-instance="socketInstance" :share-code="shareCode" />
  </n-watermark>
</template>

<script setup lang="ts">
import Terminal from '@/components/Terminal/index.vue';

import { useI18n } from 'vue-i18n';
import { onMounted, ref } from 'vue';
import { dialogContent } from './dialogContent';
import { useDialog, useMessage } from 'naive-ui';
import { useWebSocketManager } from '@/hooks/useWebSocketManager';

const { t } = useI18n();
const dialog = useDialog();
const message = useMessage();
const { createSocket }: { createSocket: () => WebSocket | '' } = useWebSocketManager();

const shareCode = ref<string>('');
const socketInstance = ref<WebSocket | ''>('');

onMounted(() => {
  const contentInstance = dialogContent();

  socketInstance.value = createSocket();

  dialog.create({
    showIcon: false,
    closable: false,
    closeOnEsc: false,
    maskClosable: false,
    title: t('VerifyCode'),
    positiveText: t('ConfirmBtn'),
    positiveButtonProps: {
      size: 'small',
      type: 'primary'
    },
    content: contentInstance.render,
    onPositiveClick: () => {
      shareCode.value = contentInstance.getValue();

      if (!shareCode.value) {
        message.error(t('InputVerifyCode'));
        return false;
      }

      return false;
    }
  });
});
</script>
