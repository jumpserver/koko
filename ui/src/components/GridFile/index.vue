<template>
  <n-flex
    vertical
    align="center"
    justify="center"
    class="w-36 h-28 rounded-sm cursor-pointer hover:bg-[rgba(255,255,255,0.1)] transition-all duration-300"
  >
    <div class="relative w-14 h-12.5 rounded-sm z-1">
      <div
        class="absolute inset-0 w-10 h-11 bg-[#145C8B] rounded-sm z-1"
        style="clip-path: polygon(0% 0, 75% 0, 100% 100%, 0 100%)"
      ></div>
      <div class="absolute top-1.5 w-14 h-11 bg-[#1C84C6] rounded-sm overflow-hidden z-10"></div>
    </div>

    <n-ellipsis style="max-width: 110px" class="text-sm text-center text-white">
      {{ fileName }}
    </n-ellipsis>
  </n-flex>
</template>

<script setup lang="ts">
defineProps<{
  fileName: string;
}>();
</script>
