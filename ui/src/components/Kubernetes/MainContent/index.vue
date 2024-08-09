<template>
  <n-layout :native-scrollbar="false" content-style="height: 100%">
    <n-tabs
      closable
      size="small"
      type="card"
      tab-style="min-width: 80px;"
      v-model:value="nameRef"
      @close="handleClose"
      class="header-tab relative"
    >
      <n-tab-pane
        v-for="panel of panels"
        :key="panel.name"
        :tab="panel.tab"
        :name="panel.name"
        class="bg-[#101014] pt-0"
      >
        <n-scrollbar trigger="hover">
          <div id="terminal" class="terminal-container"></div>
        </n-scrollbar>
      </n-tab-pane>
      <template v-slot:suffix>
        <n-flex
          justify="space-between"
          align="center"
          class="h-[35px] mr-[15px]"
          style="column-gap: 5px"
        >
          <n-popover>
            <template #trigger>
              <div
                class="icon-item flex justify-center items-center w-[25px] h-[25px] cursor-pointer transition-all duration-300 ease-in-out hover:rounded-[5px]"
              >
                <svg-icon name="split" :icon-style="iconStyle" />
              </div>
            </template>
            拆分
          </n-popover>

          <n-popover>
            <template #trigger>
              <div
                class="icon-item flex justify-center items-center w-[25px] h-[25px] cursor-pointer transition-all duration-300 ease-in-out hover:rounded-[5px]"
              >
                <n-icon size="16px" :component="EllipsisHorizontal" />
              </div>
            </template>
            操作
          </n-popover>
        </n-flex>
      </template>
    </n-tabs>
  </n-layout>
</template>

<script setup lang="ts">
import { ref, reactive, nextTick, onMounted } from 'vue';
import { CSSProperties } from 'vue';
import { useMessage } from 'naive-ui';
import { EllipsisHorizontal } from '@vicons/ionicons5';
import { useLogger } from '@/hooks/useLogger.ts';
import mittBus from '@/utils/mittBus.ts';
import { useTerminalStore } from '@/store/modules/terminal.ts';
import { useTerminal } from '@/hooks/useTerminal.ts';
import type { TabPaneProps } from 'naive-ui';
import type { ILunaConfig } from '@/hooks/interface';

// 图标样式
const iconStyle: CSSProperties = {
  width: '16px',
  height: '16px',
  transition: 'fill 0.3s'
};

// 获取 props
const props = defineProps<{
  terminalId: string;
  socket: WebSocket | null;
}>();

// 创建消息和日志实例
const message = useMessage();
const { debug } = useLogger('K8s-Terminal');

// 终端相关函数
const { createTerminal, initTerminalEvent } = useTerminal(ref(props.terminalId), 'k8s');

// 相关状态
const nameRef = ref();
const lunaConfig = ref<ILunaConfig>({});
const panels: TabPaneProps[] = reactive([]);

// 处理关闭标签页事件
const handleClose = (name: string) => {
  message.info(`已关闭: ${name}`);
  const index = panels.findIndex(panel => panel.name === name);
  panels.splice(index, 1);
};

// 创建 K8s 终端
const createK8sTerminal = async (name?: string) => {
  await nextTick();

  const el: HTMLElement = document.getElementById('terminal')!;

  const terminalStore = useTerminalStore();
  lunaConfig.value = terminalStore.getConfig;

  const { terminal, fitAddon } = createTerminal(el, lunaConfig.value);

  if (props.socket) {
    initTerminalEvent(props.socket, el, terminal, lunaConfig.value);
  }

  terminal.write('Welcome!!!');
};

// 监听连接终端事件
onMounted(() => {
  mittBus.on('connect-terminal', currentNode => {
    panels.push({
      name: currentNode.key,
      tab: currentNode.label
    });

    nameRef.value = currentNode.key;

    debug('currentNode', currentNode);

    createK8sTerminal(currentNode.key as string);
  });
});
</script>

<style scoped lang="scss">
@import './index.scss';
</style>
