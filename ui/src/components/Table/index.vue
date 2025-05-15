<template>
  <n-card>
    <n-data-table :bordered="false" :columns="columns" :data="data" />
  </n-card>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import { useTable } from '@/hooks/useTable';

import type { RowData } from '@/types/modules/table.type';
import type { DataTableColumns } from 'naive-ui';

interface Song {
  no: number;
  title: string;
  length: string;
}

const props = defineProps<{
  data: RowData[];
}>();

const { t } = useI18n();
const { createColumns } = useTable();

const columns = ref<DataTableColumns<RowData>>(createColumns());

watch(
  () => props.data,
  data => {
    console.log(data);
    if (data) {
    }
  },
  {
    immediate: true
  }
);
</script>
