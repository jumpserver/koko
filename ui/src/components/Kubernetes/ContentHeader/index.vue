<script setup lang="ts">
import { computed } from 'vue';
import { storeToRefs } from 'pinia';

import { useColor } from '@/hooks/useColor';
import { useParamsStore } from '@/store/modules/params.ts';

const paramsStore = useParamsStore();
const { lighten } = useColor();
const { setting } = storeToRefs(paramsStore);

const themeColors = computed(() => {
  const colors = {
    '--header-bg-color': lighten(5),
  };

  return colors;
});
</script>

<template>
  <n-flex
    align="center"
    class="h-[35px]"
    style="flex-wrap: nowrap; background-color: var(--header-bg-color)"
    :style="themeColors"
  >
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
