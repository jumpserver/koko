<script setup lang="ts">
import { computed } from 'vue';
import { useI18n } from 'vue-i18n';
import { storeToRefs } from 'pinia';

import { useConnectionStore } from '@/store/modules/useConnection';

const { t } = useI18n();

const connectionStore = useConnectionStore();

const { user, account, asset, protocol, date_start, date_end } = storeToRefs(connectionStore);

const descriptions = computed(() => {
  return [
    {
      label: t('User'),
      value: user?.value || '-',
    },
    {
      label: t('AccountMessage'),
      value: account?.value || '-',
    },
    {
      label: t('Asset'),
      value: asset?.value || '-',
    },
    {
      label: t('Protocol'),
      value: protocol?.value || '-',
    },
    {
      label: t('DateStart'),
      value: date_start?.value || '-',
    },
    {
      label: t('DateEnd'),
      value: date_end?.value || '-',
    },
  ];
});
</script>

<template>
  <n-descriptions bordered label-placement="left" :column="1">
    <n-descriptions-item v-for="item in descriptions" :key="item.label" :label="item.label">
      {{ item.value }}
    </n-descriptions-item>
  </n-descriptions>
</template>
