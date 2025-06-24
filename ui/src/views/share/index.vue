<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { onMounted, ref } from 'vue';
import { useDialog, useMessage } from 'naive-ui';

import Terminal from '@/components/Terminal/index.vue';

import { dialogContent } from './dialogContent';

const { t } = useI18n();
const dialog = useDialog();
const message = useMessage();

const shareCode = ref<string>('');

onMounted(() => {
  const contentInstance = dialogContent();

  dialog.create({
    showIcon: false,
    closable: false,
    closeOnEsc: false,
    maskClosable: false,
    title: t('VerifyCode'),
    positiveText: t('ConfirmBtn'),
    positiveButtonProps: {
      size: 'small',
      type: 'primary',
    },
    content: contentInstance.render,
    onPositiveClick: () => {
      shareCode.value = contentInstance.getValue();

      if (!shareCode.value) {
        message.error(t('InputVerifyCode'));
        return false;
      }

      return true;
    },
  });
});
</script>

<template>
  <Terminal v-if="shareCode" :share-code="shareCode" />
</template>
