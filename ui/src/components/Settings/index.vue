<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { onMounted, onUnmounted, ref } from 'vue';

import type { ISettingProp } from '@/types';

import mittBus from '@/utils/mittBus';

withDefaults(
  defineProps<{
    settings: ISettingProp[];
  }>(),
  {},
);
const { t } = useI18n();
const showDrawer = ref<boolean>(false);
onMounted(() => {
  mittBus.on('open-setting', () => {
    showDrawer.value = !showDrawer.value;
  });
});
onUnmounted(() => {
  mittBus.off('open-setting');
});
</script>

<template>
  <n-drawer v-model:show="showDrawer" :width="260">
    <n-drawer-content :native-scrollbar="false" :title="t('Settings')" closable>
      <n-flex vertical justify="center" align="start">
        <template v-for="item of settings" :key="item.title">
          <n-button
            v-if="!item.content"
            quaternary
            class="!w-full !justify-start"
            :disabled="item.disabled()"
            @click="item.click"
          >
            <n-icon size="18" :component="item.icon" class="mr-[10px]" />
            <n-text>
              {{ item.title }}
            </n-text>
          </n-button>
          <!-- 用户 -->
          <n-list v-else-if="item.label === 'User'" class="mt-[-15px]" clickable>
            <n-list-item>
              <n-thing class="ml-[15px] mt-[10px]">
                <template #header>
                  <n-flex align="center" justify="center">
                    <n-icon :component="item.icon" :size="18" />
                    <n-text class="text-[14px]">
                      {{ item.title }}
                      {{ `(${item?.content() ? item?.content().length : 0})` }}
                    </n-text>
                  </n-flex>
                </template>
                <template #description>
                  <n-flex size="small" style="margin-top: 4px">
                    <n-popover v-for="detail of item.content()" :key="detail.name" trigger="hover" placement="top">
                      <template #trigger>
                        <n-tag
                          round
                          strong
                          size="small"
                          class="mt-[2.5px] mb-[2.5px] mx-[25px] w-[170px] justify-around cursor-pointer overflow-hidden whitespace-nowrap text-ellipsis"
                          :bordered="false"
                          :type="item.content().indexOf(detail) !== 0 ? 'info' : 'success'"
                          :closable="true"
                          :disabled="item.content().indexOf(detail) === 0"
                          @close="item.click(detail)"
                        >
                          <n-text class="cursor-pointer text-inherit">
                            {{ detail.name }}
                          </n-text>
                          <template #icon>
                            <n-icon :component="detail.icon" />
                          </template>
                        </n-tag>
                      </template>
                      <template #default>
                        <span>{{ detail.tip }} {{ detail.name }}</span>
                      </template>
                    </n-popover>
                  </n-flex>
                </template>
              </n-thing>
            </n-list-item>
          </n-list>
          <!-- 快捷键 -->
          <n-list v-else-if="item.label === 'Keyboard'" class="mt-[-15px]" clickable>
            <n-list-item>
              <n-thing class="ml-[15px] mt-[10px]">
                <template #header>
                  <n-flex align="center" justify="center">
                    <n-icon :component="item.icon" :size="18" />
                    <n-text class="text-[14px]">
                      {{ item.title }}
                    </n-text>
                  </n-flex>
                </template>
                <template #description>
                  <n-flex size="small" style="margin-top: 4px">
                    <n-popover v-for="detail of item.content" :key="detail.name" trigger="hover" placement="top">
                      <template #trigger>
                        <n-tag
                          round
                          strong
                          type="info"
                          size="small"
                          class="mt-[2.5px] mb-[2.5px] mx-[25px] w-[170px] cursor-pointer"
                          :bordered="false"
                          :closable="false"
                          @click="detail.click()"
                        >
                          <n-text class="cursor-pointer text-inherit">
                            {{ detail.name }}
                          </n-text>
                          <template #icon>
                            <n-icon size="16" class="ml-[5px] mr-[5px]" :component="detail.icon" />
                          </template>
                        </n-tag>
                      </template>
                      <template #default>
                        <span>{{ detail.tip }}</span>
                      </template>
                    </n-popover>
                  </n-flex>
                </template>
              </n-thing>
            </n-list-item>
          </n-list>
        </template>
      </n-flex>
    </n-drawer-content>
  </n-drawer>
</template>

<style scoped lang="scss">
:deep(.n-tag__content) {
  text-overflow: ellipsis;
  overflow: hidden;
}
</style>
