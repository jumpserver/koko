<template>
  <n-flex justify="space-between" align="center" class="w-full h-full px-2 ">

    <n-space align="center">
      <n-text depth="1" class="text-xl">
        GPT-4
      </n-text>
    </n-space>

    <n-space align="center" class="!gap-x-4">
      <SquareArrowOutUpRight :size="20" class="icon-hover-primary" />
      <Trash2 :size="20" class="icon-hover-danger" />
    </n-space>


  </n-flex>
</template>

<script setup lang="ts">
import { SquareArrowOutUpRight, Trash2 } from 'lucide-vue-next';
</script>