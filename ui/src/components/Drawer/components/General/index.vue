<script setup lang="ts">
import type { FunctionalComponent } from 'vue';

import { useI18n } from 'vue-i18n';
import { useMessage } from 'naive-ui';
import { onMounted, reactive } from 'vue';
import { ArrowDown, ArrowLeft, ArrowRight, ArrowUp } from 'lucide-vue-next';

// import type { TerminalSessionInfo } from '@/types/modules/postmessage.type';
import mittBus from '@/utils/mittBus';
import { useTreeStore } from '@/store/modules/tree';
import { useTerminalStore } from '@/store/modules/terminal';
import { useSessionAdapter } from '@/hooks/useSessionAdapter';
// import { useTerminalEvents } from '@/hooks/useTerminalEvents';
import CardContainer from '@/components/CardContainer/index.vue';

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
// const { onTerminalSession } = useTerminalEvents();

// const assetName = ref('');
// const accontName = ref('');

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
      message.error(t('NoActiveTerminalTabFound'));
      return;
    }

    const currentNode = treeStore.getTerminalByK8sId(currentTab);
    const terminal = currentNode?.terminal;

    if (!terminal) {
      message.error(t('TerminalInstanceNotFound'));
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

onMounted(() => {
  // const off = onTerminalSession((info: TerminalSessionInfo) => {
  // 这里拿到终端会话数据 info
  //   console.log('session info:', info);
  //   assetName.value = info.session.asset;
  //   accontName.value = info.session.user;
  // });
});
</script>

<template>
  <n-flex vertical align="center">
    <!-- <CardContainer title="连接详情">
      <n-descriptions label-placement="left" bordered :column="1">
        <n-descriptions-item label="IP"> 苹果 </n-descriptions-item>
        <n-descriptions-item label="资产名称">
          {{ assetName }}
        </n-descriptions-item>
        <n-descriptions-item label="账号名称">
          {{ accontName }}
        </n-descriptions-item>
        <n-descriptions-item label="最大空闲时间"> 苹果 </n-descriptions-item>
        <n-descriptions-item label="授权过期时间"> 苹果 </n-descriptions-item>
        <n-descriptions-item label="最大会话时间"> 苹果 </n-descriptions-item>
        <n-descriptions-item label="当前已连接时间"> 苹果 </n-descriptions-item>
      </n-descriptions>
    </CardContainer> -->

    <CardContainer :title="t('AvailableShortcutKey')">
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
                <component :is="item.icon" :size="16" class="text-white/90 flex-shrink-0" />

                <n-text class="text-xs-plus text-white/90">
                  {{ item.label }}
                </n-text>
              </n-flex>
            </template>
          </n-card>
        </n-gi>
      </n-grid>
    </CardContainer>
  </n-flex>
</template>
