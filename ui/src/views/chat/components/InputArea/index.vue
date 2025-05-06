<template>
  <n-flex vertical class="w-full h-full flex-1 px-4 py-4 border-t-1 border-[#1a1a1a]">
    <n-flex align="center" justify="end" class="w-full h-8">
      <Eraser :size="20" class="icon-hover-primary" />
    </n-flex>

    <textarea v-model="inputValue" class="w-full h-full resize-none outline-none" />

    <!-- <n-input v-model="inputValue" type="textarea" class="w-full h-full resize-none outline-none" /> -->

    <n-flex justify="end" align="center">
      <n-text depth="1"> </n-text>
      <n-button type="primary" class="!w-18 !h-8"> 发送 </n-button>
    </n-flex>
  </n-flex>
</template>

<script setup lang="ts">
import { ArrowEnterLeft20Filled } from '@vicons/fluent';
import { Eraser } from 'lucide-vue-next';
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
