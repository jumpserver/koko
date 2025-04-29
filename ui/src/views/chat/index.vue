<template>
  <n-layout has-sider class="w-full h-full">
    <n-layout-sider
      :native-scrollbar="false"
      :collapsed-width="64"
      bordered
      show-trigger="bar"
      content-style="padding: 24px;"
    >
      <n-input placeholder="随便搜搜"></n-input>

      <n-flex align="center" class="chat-item active w-full h-12 px-2 mt-4 rounded-sm cursor-pointer">
        随便聊聊
      </n-flex>
      <n-flex align="center" class="chat-item w-full h-12 px-2 mt-4 rounded-sm cursor-pointer"> 随便聊聊 </n-flex>
      <n-flex align="center" class="chat-item w-full h-12 px-2 mt-4 rounded-sm cursor-pointer"> 随便聊聊 </n-flex>
      <n-flex align="center" class="chat-item w-full h-12 px-2 mt-4 rounded-sm cursor-pointer"> 随便聊聊 </n-flex>
      <n-flex align="center" class="chat-item w-full h-12 px-2 mt-4 rounded-sm cursor-pointer"> 随便聊聊 </n-flex>
    </n-layout-sider>
    <n-layout id="drawer-target" :native-scrollbar="false" content-style="width: calc(100% - 64px); height: 100vh;">
      <n-layout-header style="padding: 12px" bordered>
        <n-flex align="center">
          <n-text> 随便聊聊 </n-text>
          <n-popselect>
            <n-tag>
              <template #icon> </template>

              GPT-4
            </n-tag>
          </n-popselect>
        </n-flex>
      </n-layout-header>

      <n-layout-content class="h-[calc(100vh-54px)] w-full">
        <n-flex vertical align="center" class="h-full w-full !gap-0">
          <n-result
            class="w-full h-full flex flex-col justify-center items-center flex-1"
            status="500"
            title="500 服务器错误"
            description="服务器出错可能说明该雇更多程序员了"
          >
            <template #footer>
              <n-button>散财消灾</n-button>
            </template>
          </n-result>
          <input-area />
        </n-flex>
      </n-layout-content>
    </n-layout>
  </n-layout>
</template>

<script setup lang="ts">
import InputArea from './components/InputArea/index.vue';
</script>

<style scoped lang="scss">
.chat-item {
  &.active {
    background-color: rgba(255, 255, 255, 0.1);
  }

  &:hover {
    background-color: rgba(255, 255, 255, 0.2);
  }
}
</style>
