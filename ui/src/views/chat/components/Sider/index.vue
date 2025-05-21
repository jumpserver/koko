<template>
  <n-layout-sider
    bordered
    show-trigger="bar"
    :native-scrollbar="false"
    :collapsed-width="0"
    :content-style="layoutContentStyle"
  >
    <n-flex vertical align="center" class="h-full !gap-y-0">
      <n-space justify="center" align="center" class="w-full py-4">
        <img src="/public/icons/logo.svg" alt="logo" class="h-8 w-full object-fill" />
      </n-space>

      <n-divider dashed class="!my-0" />

      <n-space align="center" justify="center" class="w-full px-2" item-style="width: 100%">
        <conversation
          v-for="item in 1"
          :key="item"
          label="1"
          time-stamp="今天"
          @chat-share="handleChatShare"
          @chat-delete="handleChatDelete"
          @chat-rename="handleChatRename"
        />
      </n-space>
    </n-flex>

    <!-- <n-input placeholder="随便搜搜"></n-input> -->
  </n-layout-sider>
</template>

<script setup lang="ts">
import { reactive } from 'vue';
import Conversation from '../Conversation/index.vue';

import type { CSSProperties } from 'vue';

const layoutContentStyle = reactive<CSSProperties>({});

const handleChatShare = (chatId: string) => {
  console.log(chatId);
};

const handleChatDelete = (chatId: string) => {
  console.log(chatId);
};

const handleChatRename = (chatId: string) => {
  console.log(chatId);
};
</script>
