<script setup lang="ts">
import type { FunctionalComponent } from 'vue';

import { reactive } from 'vue';
import { ArrowDown, ArrowLeft, ArrowRight, ArrowUp, Ban } from 'lucide-vue-next';

interface KeyboardItem {
  icon: FunctionalComponent;
  label: string;
  keywords: string[];
  click: () => void;
}

const emit = defineEmits<{
  (e: 'writeCommand', command: string): void;
}>();

const keyboardList = reactive<KeyboardItem[]>([
  {
    icon: Ban,
    label: 'Cancel + C',
    keywords: ['Ctrl', 'C'],
    click: () => {
      writeDataToTerminal('Stop');
    },
  },
  {
    icon: ArrowUp,
    label: '向上箭头',
    keywords: ['\u2191'],
    click: () => {
      writeDataToTerminal('ArrowUp');
    },
  },
  {
    icon: ArrowDown,
    label: '向下箭头',
    keywords: ['\u2193'],
    click: () => {
      writeDataToTerminal('ArrowDown');
    },
  },
  {
    icon: ArrowLeft,
    label: '向左箭头',
    keywords: ['\u2190'],
    click: () => {
      writeDataToTerminal('ArrowLeft');
    },
  },
  {
    icon: ArrowRight,
    label: '向右箭头',
    keywords: ['\u2192'],
    click: () => {
      writeDataToTerminal('RightArrow');
    },
  },
]);

function writeDataToTerminal(type: string) {
  emit('writeCommand', type);
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
