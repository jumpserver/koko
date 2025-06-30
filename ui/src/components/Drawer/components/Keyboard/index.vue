<script setup lang="ts">
import type { FunctionalComponent } from 'vue';

import { reactive } from 'vue';
import { ArrowDown, ArrowLeft, ArrowRight, ArrowUp, Ban } from 'lucide-vue-next';

import mittBus from '@/utils/mittBus';

interface KeyboardItem {
  icon: FunctionalComponent;
  label: string;
  click: () => void;
}

const keyboardList = reactive<KeyboardItem[]>([
  {
    icon: Ban,
    label: 'Cancel + C',
    click: () => {
      writeDataToTerminal('\x03');
    },
  },
  {
    icon: ArrowUp,
    label: '向上箭头',
    click: () => {
      writeDataToTerminal('\x1B[A');
    },
  },
  {
    icon: ArrowDown,
    label: '向下箭头',
    click: () => {
      writeDataToTerminal('\x1B[B');
    },
  },
  {
    icon: ArrowLeft,
    label: '向左箭头',
    click: () => {
      writeDataToTerminal('\x1B[D');
    },
  },
  {
    icon: ArrowRight,
    label: '向右箭头',
    click: () => {
      writeDataToTerminal('\x1B[C');
    },
  },
]);

function writeDataToTerminal(type: string) {
  mittBus.emit('writeCommand', { type });
}
</script>

<template>
  <div>
    <n-divider title-placement="left" dashed class="!mb-3 !mt-0">
      <n-text depth="2" class="text-sm opacity-70"> 可用快捷键 </n-text>
    </n-divider>

    <n-grid x-gap="8" y-gap="8" :cols="2">
      <n-gi v-for="item in keyboardList" :key="item.label">
        <n-card
          hoverable
          class="cursor-pointer transition-all duration-200 border-transparent hover:border-white/20"
          :content-style="{ padding: '12px' }"
          @click="item.click"
        >
          <template #default>
            <n-flex align="center" :size="12" class="px-2 py-1">
              <component :is="item.icon" :size="20" class="text-white/90 flex-shrink-0" />

              <n-text class="text-sm text-white/90">
                {{ item.label }}
              </n-text>
            </n-flex>
          </template>
        </n-card>
      </n-gi>
    </n-grid>
  </div>
</template>
