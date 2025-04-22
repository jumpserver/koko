<template>
  <n-flex vertical class="h-full w-full px-4 py-[11px] bg-[#121515] !gap-y-0">
    <n-breadcrumb separator=">" class="flex items-center h-[40px]">
      <n-breadcrumb-item v-for="item in breadcrumbItem" :key="item"> {{ item }} </n-breadcrumb-item>
    </n-breadcrumb>

    <n-divider class="!my-2" />

    <Table :data="data" />
  </n-flex>
</template>

<script setup lang="ts">
import { computed, watchEffect, ref } from 'vue';
import Table from '@/components/Table/index.vue';

import type { RowData } from '@/types/modules/table.type';

const props = defineProps<{
  currentNodePath: string;

  data: RowData[];
}>();

const breadcrumbItem = ref<string[]>([]);

watchEffect(() => {
  breadcrumbItem.value = props.currentNodePath.split('/').filter(item => item !== '');
});
</script>
