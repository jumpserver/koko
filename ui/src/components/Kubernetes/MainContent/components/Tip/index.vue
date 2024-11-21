<template>
  <n-layout-content class="main-content" content-style="padding: 24px;" id="main-container">
    <n-list v-if="false" class="tips" :show-divider="false">
      <n-list-item v-for="(tip, index) of systemTips" :key="index">
        <n-flex align="center" style="flex-wrap: nowrap">
          <n-tag size="large" round>
            {{ tip.action }}
            <template #icon>
              <n-icon size="15px" :component="CheckmarkCircle" />
            </template>
          </n-tag>
          {{ tip.content }}
        </n-flex>
      </n-list-item>
    </n-list>
  </n-layout-content>
</template>

<script setup lang="ts">
import { reactive } from 'vue';
import { useI18n } from 'vue-i18n';
import { CheckmarkCircle } from '@vicons/ionicons5';

const { t } = useI18n();

const systemTips = reactive([
  {
    content: t('Reselect connection method'),
    action: t('Right click asset')
  },
  {
    content: t('Expand all asset'),
    action: t('Right click node')
  },
  {
    content: t('Asset tree loading method'),
    action: t('Settings or basic settings')
  },
  {
    content: t('Download the latest client'),
    action: t('Help or download')
  },
  {
    content: t('Keyboard keys'),
    action: t('Keyboard switch session')
  }
]);
</script>

<style scoped lang="scss">
.main-content {
  display: flex;
  align-items: center;
  justify-content: center;
  height: calc(100% - 35px);
  min-height: 385px;
  background-color: var(--el-main-bg-color);

  :deep(.n-layout-scroll-container) {
    display: flex;
    align-items: center;
  }
}
</style>
