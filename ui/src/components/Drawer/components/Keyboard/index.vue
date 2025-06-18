<template>
  <n-table :single-line="false">
    <thead>
      <tr>
        <th class="!text-center">{{ t('Format') }}</th>
        <th class="!text-center">{{ t('Hotkeys') }}</th>
      </tr>
    </thead>
    <tbody>
      <tr v-for="item in keyboardList" :key="item.label">
        <td>
          <n-flex justify="center" align="center" size="small">
            <component :is="item.icon" :size="18" class="focus:outline-none" />
            {{ item.label }}
          </n-flex>
        </td>
        <td class="flex gap-x-2 !justify-center">
          <n-tag
            v-for="keyword in item.keywords"
            :key="keyword"
            :bordered="false"
            @click="item.click"
            size="small"
            class="cursor-pointer"
          >
            <n-text depth="1" strong class="text-sm">
              {{ keyword }}
            </n-text>
          </n-tag>
        </td>
      </tr>
    </tbody>
  </n-table>
</template>

<script setup lang="ts">
import { reactive } from 'vue';
import { useI18n } from 'vue-i18n';
import { Ban, ArrowUp, ArrowDown, ArrowLeft, ArrowRight } from 'lucide-vue-next';

// Save, Undo2, ClipboardPaste,

import type { FunctionalComponent } from 'vue';

interface KeyboardItem {
  icon: FunctionalComponent;

  label: string;

  keywords: string[];

  click: () => void;
}

const emit = defineEmits<{
  (e: 'write-command', command: string): void;
}>();

const { t } = useI18n();

const keyboardList = reactive<KeyboardItem[]>([
  {
    icon: Ban,
    label: t('Cancel'),
    keywords: ['Ctrl', 'C'],
    click: () => {
      writeDataToTerminal('Stop');
    }
  },
  {
    icon: ArrowUp,
    label: t('UpArrow'),
    keywords: ['\u2191'],
    click: () => {
      writeDataToTerminal('ArrowUp');
    }
  },
  {
    icon: ArrowDown,
    label: t('DownArrow'),
    keywords: ['\u2193'],
    click: () => {
      writeDataToTerminal('ArrowDown');
    }
  },
  {
    icon: ArrowLeft,
    label: t('LeftArrow'),
    keywords: ['\u2190'],
    click: () => {
      writeDataToTerminal('ArrowLeft');
    }
  },
  {
    icon: ArrowRight,
    label: t('RightArrow'),
    keywords: ['\u2192'],
    click: () => {
      writeDataToTerminal('ArrowRight');
    }
  }
]);

const writeDataToTerminal = (type: string) => {
  emit('write-command', type);
};
</script>
