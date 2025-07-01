<script setup lang="ts">
import { storeToRefs } from 'pinia';

import { useParamsStore } from '@/store/modules/params.ts';

const paramsStore = useParamsStore();
const { setting } = storeToRefs(paramsStore);
</script>

<template>
  <n-flex align="center" class="h-[35px] bg-[#3C3C3C]" style="flex-wrap: nowrap">
    <n-flex>
      <n-spin :show="!setting.INTERFACE?.logo_logout" size="small" class="h-[35px]">
        <n-image
          lazy
          :src="setting.INTERFACE?.logo_logout"
          :preview-disabled="true"
          alt="logo"
          class="h-[35px] w-[35px] justify-center object-fill hover: cursor-pointer"
        />
      </n-spin>
    </n-flex>
  </n-flex>
</template>
