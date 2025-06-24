<script setup lang="ts">
import { Settings } from '@vicons/carbon';
import { useI18n } from 'vue-i18n';

const props = defineProps<{
  onClick: () => void;
}>();

const { t } = useI18n();

function handleSetting() {
  props.onClick();
}
</script>

<template>
  <n-popover placement="right" trigger="hover">
    <template #trigger>
      <n-icon
        :component="Settings"
        size="30"
        color="#646A73"
        class="hover:!text-white transition-all duration-300 cursor-pointer"
        @click="handleSetting"
      />
    </template>
    {{ t('Custom Setting') }}
  </n-popover>
</template>
