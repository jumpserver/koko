<template>
  <n-layout has-sider class="w-full h-full">
    <sider />

    <n-layout :native-scrollbar="false">
      <n-layout-header style="padding: 12px" bordered class="w-full h-16">
        <Header />
      </n-layout-header>

      <n-layout-content class="h-[calc(100vh-54px)] w-full">
        <n-flex vertical align="center" class="h-full w-full !gap-0">
          <welcome />
          <input-area />
        </n-flex>
      </n-layout-content>
    </n-layout>
  </n-layout>
</template>

<script setup lang="ts">
import Sider from './components/Sider/index.vue';
import Header from './components/Header/index.vue';
import Welcome from './components/Welcome/index.vue';
import InputArea from './components/InputArea/index.vue';

import { onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { useWebSocket } from '@vueuse/core'
import { generateWsURL } from '@/hooks/helper'

const message = useMessage()

const url = generateWsURL()
const { ws } = useWebSocket(url)

// ws.value?.onopen(() => {
//   console.log('open')
// })

// ws.value?.onmessage((message: MessageEvent) => {
//   console.log(message)
// })


onMounted(() => {

})
</script>
