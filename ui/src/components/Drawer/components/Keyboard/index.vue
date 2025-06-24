<script setup lang="ts">
import type { FunctionalComponent } from 'vue';

import { reactive } from 'vue';
import { useI18n } from 'vue-i18n';
// Save, Undo2, ClipboardPaste,
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

const { t } = useI18n();

const keyboardList = reactive<KeyboardItem[]>([
  {
    icon: Ban,
    label: t('Cancel'),
    keywords: ['Ctrl', 'C'],
    click: () => {
      writeDataToTerminal('Stop');
    },
  },
  // {
  //   icon: Save,
  //   label: t('Save'),
  //   keywords: ['Command/Ctrl', 'S'],
  //   click: () => {
  //     writeDataToTerminal('Save');
  //   }
  // },
  // {
  //   icon: ClipboardPaste,
  //   label: t('Paste'),
  //   keywords: ['Command/Ctrl', 'V'],
  //   click: () => {
  //     writeDataToTerminal('Paste');
  //   }
  // },
  // {
  //   icon: Undo2,
  //   label: t('Undo'),
  //   keywords: ['Command/Ctrl', 'Z'],
  //   click: () => {
  //     writeDataToTerminal('Undo');
  //   }
  // },
  {
    icon: ArrowUp,
    label: t('UpArrow'),
    keywords: ['\u2191'],
    click: () => {
      writeDataToTerminal('ArrowUp');
    },
  },
  {
    icon: ArrowDown,
    label: t('DownArrow'),
    keywords: ['\u2193'],
    click: () => {
      writeDataToTerminal('ArrowDown');
    },
  },
  {
    icon: ArrowLeft,
    label: t('LeftArrow'),
    keywords: ['\u2190'],
    click: () => {
      writeDataToTerminal('ArrowLeft');
    },
  },
  {
    icon: ArrowRight,
    label: t('RightArrow'),
    keywords: ['\u2192'],
    click: () => {
      writeDataToTerminal('ArrowRight');
    },
  },
]);

function writeDataToTerminal(type: string) {
  emit('writeCommand', type);
}
</script>

<template>
  <n-table :single-line="false">
    <thead>
      <tr>
        <th class="!text-center">
          {{ t('Format') }}
        </th>
        <th class="!text-center">
          {{ t('Hotkeys') }}
        </th>
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
            size="small"
            class="cursor-pointer"
            @click="item.click"
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
