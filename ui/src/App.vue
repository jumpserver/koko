<template>
  <n-config-provider :theme="darkTheme" :locale="zhCN" :date-locale="dateZhCN">
    <n-message-provider>
      <router-view v-if="i18nLoaded" />
    </n-message-provider>
  </n-config-provider>
</template>

<script setup lang="ts">
import { lang } from '@/config';
import { useI18n } from 'vue-i18n';
import { BASE_URL } from '@/config';
import { onMounted } from 'vue';
import { useLogger } from '@/hooks/useLogger.ts';
import { storeToRefs } from 'pinia';
// todo)) 与新 Luna 进行联动
import { darkTheme } from 'naive-ui';
import { alovaInstance } from '@/api';
import { useGlobalStore } from '@/store/modules/global';
import { zhCN, dateZhCN } from 'naive-ui';

const { error } = useLogger('App');
const globalStore = useGlobalStore();

const { mergeLocaleMessage } = useI18n();

const { i18nLoaded } = storeToRefs(globalStore);

const setLanguage = async (lang: string): Promise<void> => {
  try {
    const res = await alovaInstance
      .Get(`${BASE_URL}/api/v1/settings/i18n/koko/?lang=${lang}&flat=0`)
      .then(response => (response as Response).json());

    mergeLocaleMessage(lang, res[lang]);
  } catch (e) {
    error(`${e}`);
  } finally {
    globalStore.setI18nLoaded(true);
  }
};

onMounted(() => {
  // 设置语言
  setLanguage(lang);
});
</script>

<style scoped lang="scss">
.n-config-provider {
  width: 100%;
  height: 100%;
}
</style>
