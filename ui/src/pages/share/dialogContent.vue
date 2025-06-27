<script lang="ts" setup>
import { useI18n } from 'vue-i18n';

defineProps<{
  verifyValue: string;
}>();

const emit = defineEmits<{
  (e: 'update:verifyValue', value: string): void;
}>();

const { t } = useI18n();

const handleUpdateValue = (value: string) => {
  emit('update:verifyValue', value);
};
</script>

<template>
  <n-input
    :value="verifyValue"
    clearable
    size="small"
    type="password"
    show-password-on="mousedown"
    :placeholder="t('InputVerifyCode')"
    @update:value="handleUpdateValue"
  />
</template>
