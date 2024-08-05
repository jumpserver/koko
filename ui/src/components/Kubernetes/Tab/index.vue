<template>
  <n-layout-header class="header-tab relative">
    <n-space justify="space-between" align="center">
      <n-flex ref="el" style="gap: 0">
        <n-flex
          justify="center"
          align="center"
          v-for="item in list"
          class="tab-item cursor-pointer transition-all duration-300 ease-in-out"
          :key="item.id"
          :class="{
            'active-tab': item.isActive,
            'first-click': item.clickCount === 1,
            'second-click': item.clickCount === 2
          }"
          @click="handleTabClick(item)"
        >
          <n-text class="px-[10px] flex items-center">
            <span
              class="text-item inline-flex h-[35px] min-w-[85px] text-[13px] justify-center items-center"
            >
              {{ item.name }}
            </span>
            <n-icon
              class="close-icon"
              v-if="item.isActive"
              :size="14"
              :component="CloseOutline"
            ></n-icon>
          </n-text>
        </n-flex>
      </n-flex>
      <!--	todo)) 组件拆分		-->
      <n-flex
        justify="space-between"
        align="center"
        class="h-[35px] mr-[15px]"
        style="column-gap: 5px"
      >
        <n-popover>
          <template #trigger>
            <div
              class="icon-item flex justify-center items-center w-[25px] h-[25px] cursor-pointer transition-all duration-300 ease-in-out hover:rounded-[5px]"
            >
              <svg-icon name="split" :icon-style="iconStyle" />
            </div>
          </template>
          拆分
        </n-popover>

        <n-popover>
          <template #trigger>
            <div
              class="icon-item flex justify-center items-center w-[25px] h-[25px] cursor-pointer transition-all duration-300 ease-in-out hover:rounded-[5px]"
            >
              <n-icon size="16px" :component="EllipsisHorizontal" />
            </div>
          </template>
          操作
        </n-popover>
      </n-flex>
      <!--	todo)) 组件拆分		-->
    </n-space>
  </n-layout-header>
</template>

<script setup lang="ts">
import SvgIcon from '@/components/SvgIcon/index.vue';

import { type CSSProperties, reactive, ref } from 'vue';
import { EllipsisHorizontal, CloseOutline } from '@vicons/ionicons5';
import { useDraggable, type UseDraggableReturn } from 'vue-draggable-plus';

const el = ref();

const list = reactive([
  {
    name: 'index.js',
    id: 1,
    clickCount: 0,
    isActive: false
  },
  {
    name: 'Jean',
    id: 2,
    clickCount: 0,
    isActive: false
  }
]);

const iconStyle: CSSProperties = {
  width: '16px',
  height: '16px',
  transition: 'fill 0.3s'
};

// eslint-disable-next-line @typescript-eslint/no-unused-vars
const draggable = useDraggable<UseDraggableReturn>(el, list, {
  animation: 150,
  onStart() {
    console.log('start');
  },
  onUpdate() {
    console.log('update');
  }
});

const handleTabClick = (item: { id: number }) => {
  list.forEach(tab => {
    if (tab.id === item.id) {
      tab.clickCount = tab.clickCount < 2 ? tab.clickCount + 1 : 1; // 重置为1时保证重新点击
      tab.isActive = true;
    } else {
      tab.clickCount = 0;
      tab.isActive = false;
    }
  });
};
</script>

<style scoped lang="scss">
$--el-main-tab-bg-color: #252526;
$--el-main-tab-text-color: #ffffff;
$--el-main-tab-icon-color: #c5c5c5;
$--el-main-tab-icon-hover-color: #5a5d5e4f;
$--el-main-text-color: #cccccc;
$--el-main-bg-color: #1e1e1e;

.header-tab {
  width: 100% !important;
  background-color: $--el-main-tab-bg-color;

  .tab-item {
    :deep(.text-item) {
      color: $--el-main-tab-text-color;
    }

    :deep(.close-icon) {
      color: $--el-main-tab-text-color;
    }

    // todo)) 有些问题
    &.first-click {
      font-style: italic;
      color: $--el-main-text-color;
    }

    &.second-click {
      font-style: normal;
      color: rgb(255 255 255 / 50%);
    }

    &.active-tab {
      color: $--el-main-text-color !important;
      background-color: $--el-main-bg-color;
    }
  }

  :deep(.icon-item) {
    svg {
      color: $--el-main-tab-icon-color;
      fill: $--el-main-tab-icon-color;
    }

    &:hover {
      background-color: $--el-main-tab-icon-hover-color;
    }
  }
}
</style>
