<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { h, onMounted, ref } from 'vue';
import { useDialog, useMessage } from 'naive-ui';

import Terminal from '@/components/Terminal/index.vue';

import DialogContent from './dialogContent.vue';

const { t } = useI18n();
const dialog = useDialog();
const message = useMessage();

const shareCode = ref<string>('');
const verifyValue = ref<string>('');

onMounted(() => {
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
    content: () =>
      h(DialogContent, {
        verifyValue: verifyValue.value,
        onUpdateVerifyValue: (value: string) => {
          verifyValue.value = value;
        },
      }),
    onPositiveClick: () => {
      shareCode.value = verifyValue.value;

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
