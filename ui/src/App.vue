<template>
  <n-config-provider
    :locale="zhCN"
    :theme="darkTheme"
    :date-locale="dateZhCN"
    :theme-overrides="themeOverrides"
    class="flex flex-col items-center justify-center h-full w-full overflow-hidden bg-black"
  >
    <n-dialog-provider>
      <n-notification-provider>
        <n-message-provider>
          <router-view v-if="i18nLoaded" />
        </n-message-provider>
      </n-notification-provider>
    </n-dialog-provider>
  </n-config-provider>
</template>

<script setup lang="ts">
import { lang } from '@/config';
import { BASE_URL } from '@/config';
import { darkTheme } from 'naive-ui';
import { alovaInstance } from '@/api';
import { zhCN, dateZhCN } from 'naive-ui';
import { themeOverrides } from './overrides.ts';

import { onMounted } from 'vue';
import { useI18n } from 'vue-i18n';
import { storeToRefs } from 'pinia';
import { useLogger } from '@/hooks/useLogger.ts';
import { useGlobalStore } from '@/store/modules/global';

const { mergeLocaleMessage } = useI18n();
const { error } = useLogger('App');

const globalStore = useGlobalStore();
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
  setLanguage(lang);
});
</script>
