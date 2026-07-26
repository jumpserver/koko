<script setup lang="ts">
import type { FunctionalComponent } from 'vue';

import { useI18n } from 'vue-i18n';
import { useMessage } from 'naive-ui';
import { onMounted, reactive } from 'vue';
import { ArrowDown, ArrowLeft, ArrowRight, ArrowUp } from 'lucide-vue-next';

// import type { TerminalSessionInfo } from '@/types/modules/postmessage.type';
import { formatMessage } from '@/utils';
import { lunaCommunicator } from '@/utils/lunaBus';
import { useTreeStore } from '@/store/modules/tree';
import { useTerminalStore } from '@/store/modules/terminal';
import { useConnectionStore } from '@/store/modules/useConnection';
import { useSessionAdapter } from '@/hooks/useSessionAdapter';
import { FORMATTER_MESSAGE_TYPE, LUNA_MESSAGE_TYPE } from '@/types/modules/message.type';
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
const connectionStore = useConnectionStore();
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
    label: 'Ctrl+W',
    click: () => {
      writeDataToTerminal('\x17');
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
    const socket = currentNode?.socket;

    if (!terminal || !socket) {
      message.error(t('TerminalInstanceNotFound'));
      return;
    }

    socket.send(
      JSON.stringify({
        data: type,
        id: currentNode.id,
        pod: currentNode.pod || '',
        k8s_id: currentNode.k8s_id,
        namespace: currentNode.namespace || '',
        container: currentNode.container || '',
        type: FORMATTER_MESSAGE_TYPE.TERMINAL_K8S_DATA,
      })
    );
    terminal.focus();
    lunaCommunicator.sendLuna(LUNA_MESSAGE_TYPE.INPUT_ACTIVE, '');
  } else {
    const { socket, terminalId, terminal } = connectionStore;

    if (!socket || !terminalId) {
      console.error('WebSocket connection may be closed, please refresh the page');
      return;
    }

    socket.send(formatMessage(terminalId, FORMATTER_MESSAGE_TYPE.TERMINAL_DATA, type));
    terminal?.focus();
    lunaCommunicator.sendLuna(LUNA_MESSAGE_TYPE.INPUT_ACTIVE, '');
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
