<template>
  <n-flex vertical class="w-full h-full max-h-128 flex-1 px-4 py-4 border-t-1 border-[#1a1a1a]">
    <textarea v-model="inputValue" class="w-full h-full resize-none outline-none" />

    <n-flex justify="end" align="center">
      <n-text depth="1"> </n-text>
      <n-button type="primary" class="!w-18 !h-8"> 发送 </n-button>
    </n-flex>
  </n-flex>
</template>

<script setup lang="ts">
import { ArrowEnterLeft20Filled } from '@vicons/fluent';
import { ref, watch } from 'vue';

const inputValue = ref('');

watch(
  () => inputValue.value,
  val => {
    console.log(val);

    // 将以 @开头 空格结尾的文本，转为 tag
    const tag = val.match(/@[^ ]+\s/g);
  }
);
</script>
