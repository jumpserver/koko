<script setup lang="ts">
import type { FunctionalComponent } from 'vue';

import { reactive } from 'vue';
import { useI18n } from 'vue-i18n';
import { useMessage } from 'naive-ui';
import { ArrowDown, ArrowLeft, ArrowRight, ArrowUp } from 'lucide-vue-next';

import mittBus from '@/utils/mittBus';
import { useTreeStore } from '@/store/modules/tree';
import { useTerminalStore } from '@/store/modules/terminal';
import { useSessionAdapter } from '@/hooks/useSessionAdapter';

interface KeyboardItem {
  icon?: FunctionalComponent;
  label?: string;
  click: () => void;
}

const { t } = useI18n();
const message = useMessage();
const treeStore = useTreeStore();
const terminalStore = useTerminalStore();
const { isK8sEnvironment } = useSessionAdapter();

const keyboardList = reactive<KeyboardItem[]>([
  {
    // icon: Ban,
    label: 'Ctrl+C',
    click: () => {
      writeDataToTerminal('\x03');
    },
  },
  {
    icon: ArrowUp,
    // label: t('UpArrow'),
    click: () => {
      writeDataToTerminal('\x1B[A');
    },
  },
  {
    icon: ArrowDown,
    // label: t('DownArrow'),
    click: () => {
      writeDataToTerminal('\x1B[B');
    },
  },
  {
    icon: ArrowLeft,
    // label: t('LeftArrow'),
    click: () => {
      writeDataToTerminal('\x1B[D');
    },
  },
  {
    icon: ArrowRight,
    // label: t('RightArrow'),
    click: () => {
      writeDataToTerminal('\x1B[C');
    },
  },
]);

function writeDataToTerminal(type: string) {
  if (isK8sEnvironment.value) {
    // K8s 环境：根据当前 tab 获取对应的 terminal 实例
    const currentTab = terminalStore.currentTab;

    if (!currentTab) {
      message.error('No active terminal tab found');
      return;
    }

    const currentNode = treeStore.getTerminalByK8sId(currentTab);
    const terminal = currentNode?.terminal;

    if (!terminal) {
      message.error('Terminal instance not found for current tab');
      return;
    }

    // 直接向当前活跃的终端写入内容
    terminal.paste(type);
    terminal.focus();
  } else {
    // 普通连接：使用原有的 mittBus 事件机制
    mittBus.emit('write-command', { type });
  }
}
</script>

<template>
  <div>
    <n-divider title-placement="left" dashed class="!mb-3 !mt-0">
      <n-text depth="2" class="text-sm opacity-70">
        {{ t('AvailableShortcutKey') }}
      </n-text>
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
            <n-flex align="center" justify="center" :size="12" class="!gap-0">
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
