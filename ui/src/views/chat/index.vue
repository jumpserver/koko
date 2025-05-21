<template>
  <n-layout has-sider class="w-full h-full">
    <sider />

    <n-layout :native-scrollbar="false">
      <n-layout-header style="padding: 12px" bordered class="w-full h-16">
        <Header />
      </n-layout-header>

      <n-layout-content :content-style="{ height: 'calc(100vh - 4rem)' }">
        <n-split direction="vertical" :default-size="0.66" :max="0.9" :resize-trigger-size="2" class="h-full w-full">
          <template #1> 
            <!-- <welcome /> -->
            <content />
          </template>
          <template #2> <input-area @send-message="handleSendMessage" /> </template>
        </n-split>
      </n-layout-content>
    </n-layout>
  </n-layout>
</template>

<script setup lang="ts">
import Sider from './components/Sider/index.vue';
import Header from './components/Header/index.vue';
import Welcome from './components/Welcome/index.vue';
import Content from './components/Content/index.vue';
import InputArea from './components/InputArea/index.vue';

import { useMessage } from 'naive-ui';
import { useChat } from '@/hooks/useChat.ts';

import type { ChatSendMessage } from '@/types/modules/chat.type';

const message = useMessage();
const { createChatSocket, sendChatMessage } = useChat();

createChatSocket();

const handleSendMessage = (message: ChatSendMessage) => {
  console.log(message);
  sendChatMessage(message);
};
</script>
