<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { computed, ref } from 'vue';
import { Settings } from 'lucide-vue-next';
import { Kubernetes } from '@vicons/carbon';

import mittBus from '@/utils/mittBus';
import { useColor } from '@/hooks/useColor';

const { t } = useI18n();
const { lighten, darken } = useColor();

const isActive = ref(true);

const themeColors = computed(() => {
  const colors = {
    '--sidebar-icon-color': lighten(45),
    '--sidebar-icon-hover-color': lighten(55),
    '--sidebar-icon-active-color': lighten(60),
    '--sidebar-active-border-color': '#1ab394',
    '--sidebar-hover-bg-color': lighten(6),
    backgroundColor: darken(5),
  };

  return colors;
});

const handleTreeIconClick = () => {
  mittBus.emit('fold-tree-click');
  isActive.value = !isActive.value;
};

const handleOpenSetting = () => {
  mittBus.emit('open-setting');
};
</script>

<template>
  <n-flex justify="center" align="center" class="cursor-pointer w-full h-[45px]" :style="themeColors">
    <n-button
      text
      class="py-[5px] w-full icon-wrapper"
      :class="{ active: isActive }"
      :style="{ backgroundColor: darken(4) }"
      @click="handleTreeIconClick"
    >
      <n-icon
        :component="Kubernetes"
        size="30"
        class="hover:!text-white transition-all duration-300 cursor-pointer tree-icon"
      />
    </n-button>
  </n-flex>
  <n-flex justify="center" align="center" class="mb-[5px] cursor-pointer w-[45px] h-[45px]">
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
      color: var(--sidebar-icon-active-color) !important;
    }

    &::before {
      position: absolute;
      top: 0;
      left: 0;
      display: block;
      width: 2px;
      height: 100%;
      content: '';
      background-color: var(--sidebar-active-border-color);
    }
  }
}

:deep(.n-icon) {
  color: var(--sidebar-icon-color);
  transition: color 0.3s ease;

  &:hover {
    color: var(--sidebar-icon-hover-color);
  }
}
</style>
