<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { useMessage } from 'naive-ui';
import { useRoute } from 'vue-router';
import { nextTick, onMounted, ref } from 'vue';

import Terminal from '@/components/Terminal/index.vue';
import { useConnectionStore } from '@/store/modules/useConnection';
import TerminalProvider from '@/components/TerminalProvider/index.vue';

const { t } = useI18n();
const route = useRoute();
const message = useMessage();
const connectionStore = useConnectionStore();

const verifyValue = ref<string>('');
const showModal = ref<boolean>(false);

const onFinish = () => {
  if (!verifyValue.value) {
    message.error(t('Please input verify code'));
    return false;
  }

  connectionStore.setConnectionState({
    shareCode: verifyValue.value,
  });

  nextTick(() => {
    showModal.value = false;
  });

  return true;
};

onMounted(() => {
  const { code } = route.query;

  if (code && code !== 'undefined') {
    connectionStore.setConnectionState({
      shareCode: code as string,
    });

    nextTick(() => {
      showModal.value = false;
    });
  } else {
    showModal.value = true;
  }
});
</script>

<template>
  <n-modal
    v-model:show="showModal"
    preset="dialog"
    :title="t('VerifyCode')"
    :closable="false"
    :show-icon="false"
    :close-on-esc="false"
    :mask-closable="false"
    :positive-text="t('Confirm')"
    @positive-click="onFinish"
  >
    <n-input
      v-model:value="verifyValue"
      clearable
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
