<template>
  <n-config-provider
    :locale="zhCN"
    :theme="darkTheme"
    :date-locale="dateZhCN"
    :theme-overrides="themeOverrides"
    class="flex items-center justify-center h-full w-full overflow-hidden bg-black"
  >
    <n-dialog-provider>
      <n-notification-provider>
        <n-message-provider>
          <router-view />
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

const { mergeLocaleMessage } = useI18n();

onMounted(async () => {
  try {
    const translations = await alovaInstance
      .Get(`${BASE_URL}/api/v1/settings/i18n/koko/?lang=${lang}&flat=0`)
      .then(response => (response as Response).json());

    if (translations[lang]) {
      mergeLocaleMessage(lang, translations[lang]);
    }
  } catch (e) {
    throw new Error(`${e}`);
  }
});
</script>
