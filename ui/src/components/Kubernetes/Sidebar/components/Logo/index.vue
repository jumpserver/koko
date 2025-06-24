<script setup lang="ts">
defineProps<{
  logoImage: string | undefined;
}>();
</script>

<template>
  <n-spin :show="!logoImage" size="small">
    <n-image lazy :src="logoImage" alt="" class="h-[30px] w-[30px] justify-center object-fill hover: cursor-pointer" />
  </n-spin>
</template>
