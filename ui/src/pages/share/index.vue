<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { nextTick, onMounted, ref } from 'vue';

import Terminal from '@/components/Terminal/index.vue';
import { useConnectionStore } from '@/store/modules/useConnection';
import TerminalProvider from '@/components/TerminalProvider/index.vue';

const { t } = useI18n();
const connectionStore = useConnectionStore();

const verifyValue = ref<string[]>([]);
const showModal = ref<boolean>(false);

const onFinish = () => {
  connectionStore.setConnectionState({
    shareCode: verifyValue.value.join(''),
  });

  nextTick(() => {
    showModal.value = false;
  });
};

onMounted(() => {
  showModal.value = true;
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
  >
    <n-input-otp v-model:value="verifyValue" :length="4" size="large" class="justify-center pb-3" @finish="onFinish" />
  </n-modal>

  <TerminalProvider v-if="!showModal">
    <template #terminal>
      <Terminal />
    </template>
  </TerminalProvider>
</template>
