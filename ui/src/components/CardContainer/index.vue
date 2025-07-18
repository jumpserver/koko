<script setup lang="ts">
import { ref } from 'vue';
import { ChevronDown, ChevronLeft } from 'lucide-vue-next';

const props = defineProps<{
  title?: string;
}>();

const collapseStatus = ref(false);

const handleCollapse = ({ expanded }: { expanded: boolean }) => {
  collapseStatus.value = expanded;
};
</script>

<template>
  <n-card bordered>
    <n-collapse :trigger-areas="['extra', 'main']" :default-expanded-names="['1']" @item-header-click="handleCollapse">
      <n-collapse-item :title="title" name="1">
        <slot name="default" />

        <template v-if="!title" #header>
          <slot name="custom-header" />
        </template>

        <template #header-extra>
          <ChevronLeft v-if="!collapseStatus" :size="16" />
          <ChevronDown v-else :size="16" />
        </template>
      </n-collapse-item>
    </n-collapse>
  </n-card>
</template>

<style scoped lang="scss">
.n-collapse .n-collapse-item {
  :deep(.n-collapse-item__header .n-collapse-item-arrow) {
    display: none;
  }
}
</style>
