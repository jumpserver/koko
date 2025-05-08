<template>
  <n-thing :content-style="{ marginTop: 'unset' }">
    <template #header>
      <n-text depth="1" class="text-xs px-2">今天</n-text>
    </template>

    <template #default>
      <n-list hoverable clickable :show-divider="false" class="w-full">
        <n-list-item class="h-9 !py-0 !px-2">
          随便聊聊

          <template #suffix>
            <n-popselect animated show-arrow size="small" trigger="click" :options="OptionRender(emits)">
              <Ellipsis :size="20" class="icon-hover-primary" />
            </n-popselect>
          </template>
        </n-list-item>
      </n-list>
    </template>
  </n-thing>

  <n-thing :content-style="{ marginTop: 'unset' }" class="mt-5">
    <template #header>
      <n-text depth="1" class="text-xs px-2">昨天</n-text>
    </template>

    <template #default>
      <n-list hoverable clickable :show-divider="false" class="w-full">
        <n-list-item class="h-9 !py-0 !px-2">
          随便聊聊

          <template #suffix>
            <n-popselect animated show-arrow size="small" trigger="click" :options="OptionRender(emits)">
              <Ellipsis :size="20" class="icon-hover-primary" />
            </n-popselect>
          </template>
        </n-list-item>
      </n-list>
    </template>
  </n-thing>
</template>

<script setup lang="ts">
import { Ellipsis } from 'lucide-vue-next';
import { OptionRender } from './optionRender.tsx';

const props = defineProps<{
  label: string;

  timeStamp: string;
}>();

const emits = defineEmits<{
  (e: 'chat-share', shareId: string): void;
  (e: 'chat-rename', shareId: string): void;
  (e: 'chat-delete', shareId: string): void;
}>();
</script>

<style scoped lang="scss">
.chat-item {
  &.active {
    background-color: rgba(255, 255, 255, 0.1);
  }

  &:hover {
    background-color: rgba(255, 255, 255, 0.2);
  }
}
</style>
