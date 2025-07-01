<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { onMounted, ref } from 'vue';
import { useMessage } from 'naive-ui';

import Terminal from '@/components/Terminal/index.vue';

const { t } = useI18n();
const message = useMessage();

const shareCode = ref<string>('');
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

  shareCode.value = verifyValue.value;
  showModal.value = false;
};
</script>

<template>
  <div>
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

    <Terminal v-if="shareCode" :share-code="shareCode" />
  </div>
</template>
