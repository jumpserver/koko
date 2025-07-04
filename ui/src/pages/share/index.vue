<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { useMessage } from 'naive-ui';
import { nextTick, onMounted, ref } from 'vue';

import Terminal from '@/components/Terminal/index.vue';
import { useConnectionStore } from '@/store/modules/useConnection';
import TerminalProvider from '@/components/TerminalProvider/index.vue';

const { t } = useI18n();
const message = useMessage();
const connectionStore = useConnectionStore();

const verifyValue = ref<string>('');
const showModal = ref<boolean>(false);

onMounted(() => {
  showModal.value = true;
});

const handleConfirm = () => {
  if (!verifyValue.value) {
    message.error(t('InputVerifyCode'));
    return;
  }

  connectionStore.setConnectionState({
    shareCode: verifyValue.value,
  });

  nextTick(() => {
    showModal.value = false;
  });
};
</script>

<template>
  <n-modal
    v-model:show="showModal"
    preset="dialog"
    :title="t('VerifyCode')"
    :positive-text="t('ConfirmBtn')"
    :closable="false"
    :close-on-esc="false"
    :mask-closable="false"
    @positive-click="handleConfirm"
  >
    <n-input
      v-model:value="verifyValue"
      clearable
      size="small"
      type="password"
      show-password-on="mousedown"
      :placeholder="t('InputVerifyCode')"
    />
  </n-modal>

  <TerminalProvider v-if="!showModal">
    <template #terminal>
      <Terminal />
    </template>
  </TerminalProvider>
</template>
