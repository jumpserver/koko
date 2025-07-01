<script setup lang="ts">
import { ref } from 'vue';
import { useI18n } from 'vue-i18n';
import { Settings } from 'lucide-vue-next';
import { Kubernetes } from '@vicons/carbon';

import mittBus from '@/utils/mittBus';

const { t } = useI18n();

const isActive = ref(true);

const handleOpenSetting = () => {
  mittBus.emit('open-setting');
};

const handleTreeIconClick = () => {
  mittBus.emit('fold-tree-click');
  isActive.value = !isActive.value;
};
</script>

<template>
  <n-flex justify="cent er" align="center" class="cursor-pointer w-full h-[48px]">
    <n-button text class="py-[5px] w-full icon-wrapper" :class="{ active: isActive }" @click="handleTreeIconClick">
      <n-icon
        :component="Kubernetes"
        size="30"
        class="hover:!text-white transition-all duration-300 cursor-pointer tree-icon"
      />
    </n-button>
  </n-flex>
  <n-flex justify="center" align="center" class="mb-[5px] cursor-pointer w-[48px] h-[48px]">
    <n-popover placement="right" trigger="hover">
      <template #trigger>
        <Settings
          :size="30"
          class="icon-hover transition-all duration-300 cursor-pointer focus:outline-none"
          @click="handleOpenSetting"
        />
      </template>
      {{ t('Custom Setting') }}
    </n-popover>
  </n-flex>
</template>

<style scoped lang="scss">
:deep(.n-flex) {
  gap: 15px 12px !important;
}

.icon-wrapper {
  position: relative;
  height: 100%;
  width: 100%;

  &.active {
    .tree-icon {
      color: #ffffff !important;
    }

    &::before {
      position: absolute;
      top: 0;
      left: 0;
      display: block;
      width: 2px;
      height: 100%;
      content: '';
      background-color: #1ab394;
    }
  }
}
</style>
