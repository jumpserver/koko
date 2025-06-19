<template>
  <n-card>
    <n-data-table :bordered="false" :columns="columns" :data="data" />
  </n-card>
</template>

<script setup lang="ts">
import { ref } from 'vue';
import { useTable } from '@/hooks/useTable';

import type { RowData } from '@/types/modules/table.type';
import type { DataTableColumns } from 'naive-ui';

const props = defineProps<{
  data: RowData[];
}>();


const { createColumns } = useTable();

const columns = ref<DataTableColumns<RowData>>(createColumns());

</script>
