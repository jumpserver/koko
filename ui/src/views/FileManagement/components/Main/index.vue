<template>
  <n-flex vertical class="h-full w-full px-4 py-[11px] bg-[#121515] !gap-y-0">
    <template v-if="!isGrid">
      <n-breadcrumb separator=">" class="flex items-center h-[40px]">
        <n-breadcrumb-item v-for="item in breadcrumbItem" :key="item"> {{ item }} </n-breadcrumb-item>
      </n-breadcrumb>

      <n-divider class="!my-2" />

      <Table :data="data" />
    </template>

    <template v-else>
      <n-flex ref="el" class="flex-wrap !gap-x-12 !gap-y-6 px-4 py-4">
        <GridFile v-for="item in data" :key="item.name" :fileName="item.name" @dblclick="handleEnterFile(item)" />
      </n-flex>
    </template>
  </n-flex>
</template>

<script setup lang="ts">
import { useDraggable } from 'vue-draggable-plus';
import { watchEffect, ref, onMounted, toRef } from 'vue';

import Table from '@/components/Table/index.vue';
import GridFile from '@/components/GridFile/index.vue';

import type { RowData } from '@/types/modules/table.type';

const props = defineProps<{
  currentNodePath: string;

  isGrid: boolean;

  data: RowData[];
}>();

const emits = defineEmits<{
  (e: 'enter-file', filePath: string): void;
  (e: 'back', filePath: string): void;
}>();

const el = ref(null);
const breadcrumbItem = ref<string[]>([]);

watchEffect(() => {
  breadcrumbItem.value = props.currentNodePath.split('/').filter(item => item !== '');
});

const handleEnterFile = (item: RowData) => {
  if (item.name === '..') {
    emits('back', props.currentNodePath.split('/').slice(0, -1).join('/'));
    return;
  }

  emits('enter-file', `${props.currentNodePath}/${item.name}`);
};

onMounted(() => {
  if (props.isGrid) {
    const { start } = useDraggable(el, toRef(props, 'data'), {
      animation: 150,
      ghostClass: 'ghost',
      onStart() {
        console.log('start');
      },
      onUpdate() {
        console.log('update');
      }
    });

    start();
  }
});
</script>
