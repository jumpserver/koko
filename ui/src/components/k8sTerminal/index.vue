<template>
  <n-layout :native-scrollbar="false">
    <n-scrollbar trigger="hover">
      <div id="terminal" class="terminal-container"></div>
    </n-scrollbar>
  </n-layout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useTerminalStore } from '@/store/modules/terminal.ts';
import { Terminal } from '@xterm/xterm';
import { useTreeStore } from '@/store/modules/tree.ts';
import { storeToRefs } from 'pinia';
import { useSentry } from '@/hooks/useZsentry.ts';
import { Sentry } from 'nora-zmodemjs/src/zmodem_browser';
import { base64ToUint8Array } from '@/hooks/helper';
import { useI18n } from 'vue-i18n';
import { useMessage } from 'naive-ui';
import { sendEventToLuna } from '@/components/CustomTerminal/helper';

const props = defineProps<{
  theme: string;
  socket: WebSocket;
}>();

const emits = defineEmits<{
  (e: 'socketEvent'): void;
}>();

let sentry: Sentry;
let terminalInstance: Terminal;

const { t } = useI18n();
const { createSentry } = useSentry();

const message = useMessage();
const treeStore = useTreeStore();
const terminalStore = useTerminalStore();

const k8s_id = ref('');
const terminalId = ref('');

const sendDataFromWindow = () => {};
const handleK8sMessage = (socketData: any) => {
  switch (socketData.type) {
    case 'TERMINAL_K8S_BINARY': {
      k8s_id.value = socketData.k8s_id;
      terminalId.value = socketData.id;

      sentry = createSentry();
      sentry.consume(base64ToUint8Array(socketData.raw));

      break;
    }
    case 'TERMINAL_ACTION': {
      const action = socketData.data;

      switch (action) {
        case 'ZMODEM_START': {
          message.warning(t('CustomTerminal.WaitFileTransfer'));

          break;
        }
        case 'ZMODEM_END': {
          message.warning(t('CustomTerminal.EndFileTransfer'));

          terminalInstance.writeln('\r\n');
          break;
        }
      }

      break;
    }
    case 'TERMINAL_ERROR': {
      terminalInstance.write(socketData.err);
      break;
    }
    case 'K8S_CLOSE': {
      const id = socketData.k8s_id;

      if (id) {
        treeStore.removeK8sIdMap(id);
      }

      break;
    }
  }

  emits('socketEvent');
};

const init = () => {
  const { currentNode } = storeToRefs(treeStore);
  const { fontSize, lineHeight, fontFamily } = terminalStore.getConfig;

  const terminalOptions = {
    allowProposedApi: true,
    fontSize,
    lineHeight,
    fontFamily,
    rightClickSelectsWord: true,
    theme: {
      background: '#1E1E1E'
    },
    scrollback: 5000
  };

  terminalInstance = new Terminal(terminalOptions);

  treeStore.setK8sIdMap(currentNode.value.k8s_id!, {
    terminalInstance,
    handler: (e: MessageEvent) => {
      handleK8sMessage(JSON.parse(e.data));
    },
    ...currentNode.value
  });
};
const initCustomWindowEvent = () => {
  window.addEventListener('message', (e: MessageEvent) => {
    const message = e.data;

    switch (message.name) {
      case 'PING': {
        // lunaId.value = message.id;
        // origin.value = e.origin;

        // sendEventToLuna('PONG', '', lunaId.value, origin.value);
        break;
      }
      case 'CMD': {
        sendDataFromWindow(message.data);
        break;
      }
      case 'FOCUS': {
        terminalInstance.focus();
        break;
      }
    }
  });
};

init();
initCustomWindowEvent();

onMounted(() => {
  const theme = props.theme;
  const el = document.getElementById('terminal');
});
</script>

<style scoped lang="scss"></style>
