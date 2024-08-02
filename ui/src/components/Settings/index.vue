<template>
  <div>
    <n-drawer v-model:show="showDrawer" :width="260">
      <n-drawer-content :title="t('Settings')" closable>
        <n-flex vertical>
          <template v-for="item of settings" :key="item.title">
            <n-button
              v-if="!item.content"
              quaternary
              class="justify-start items-center"
              :disabled="item.disabled()"
              @click="item.click"
            >
              <n-icon size="16" :component="item.icon" class="mr-[10px]" />
              <n-text>
                {{ item.title }}
              </n-text>
            </n-button>
            <n-list hoverable clickable v-else>
              <n-list-item>
                <n-thing content-style="margin-top: 10px;">
                  <template #header>
                    <n-flex align="center" justify="center">
                      <n-icon :component="item.icon" :size="16"></n-icon>
                      <n-text class="text-[14px]">{{ item.title }}</n-text>
                    </n-flex>
                  </template>
                  <template #description>
                    <n-space size="small" style="margin-top: 4px">
                      <n-tag
                        v-for="detail of item.content"
                        :bordered="false"
                        type="info"
                        size="small"
                      >
                        {{ detail.name }}
                      </n-tag>
                    </n-space>
                  </template>
                </n-thing>
              </n-list-item>
              <!--              <n-list-item>-->
              <!--                <n-thing title="他在时间门外" content-style="margin-top: 10px;">-->
              <!--                  <template #description>-->
              <!--                    <n-space size="small" style="margin-top: 4px">-->
              <!--                      <n-tag :bordered="false" type="info" size="small"> 环形公路 </n-tag>-->
              <!--                      <n-tag :bordered="false" type="info" size="small"> 潜水艇司机 </n-tag>-->
              <!--                    </n-space>-->
              <!--                  </template>-->
              <!--                  最新的打印机<br />-->
              <!--                  复制着彩色傀儡<br />-->
              <!--                  早上好我的罐头先生<br />-->
              <!--                  让他带你去被工厂敲击-->
              <!--                </n-thing>-->
              <!--              </n-list-item>-->
            </n-list>
          </template>
        </n-flex>
      </n-drawer-content>
    </n-drawer>
  </div>
</template>

<script setup lang="ts">
import mittBus from '@/utils/mittBus.ts';

import { onMounted, onUnmounted, ref } from 'vue';
import { ISettingProp } from '@/views/interface';
import { useI18n } from 'vue-i18n';

withDefaults(
  defineProps<{
    settings: ISettingProp[];
  }>(),
  {}
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

<style scoped lang="scss"></style>
