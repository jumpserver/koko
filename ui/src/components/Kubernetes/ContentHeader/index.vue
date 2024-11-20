<template>
  <n-flex align="center" class="h-[35px] w-full bg-[#3C3C3C]" style="flex-wrap: nowrap">
    <n-flex>
      <Logo class="ml-[10px]" :logo-image="setting.INTERFACE?.logo_logout" />
    </n-flex>
  </n-flex>
</template>

<script setup lang="ts">
import { storeToRefs } from 'pinia';
import { useParamsStore } from '@/store/modules/params.ts';

import Logo from '@/components/Kubernetes/Sidebar/components/Logo/index.vue';

const paramsStore = useParamsStore();
const { setting } = storeToRefs(paramsStore);
</script>
