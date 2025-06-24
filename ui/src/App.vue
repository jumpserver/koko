<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { nextTick, onMounted, ref } from 'vue';
import { darkTheme, dateZhCN, enUS } from 'naive-ui';

import { alovaInstance } from '@/api';
import { BASE_URL, LanguageCode } from '@/utils/config';

import { themeOverrides } from './overrides';

const { mergeLocaleMessage } = useI18n();

const loaded = ref(false);

onMounted(async () => {
  loaded.value = false;
  try {
    const translations = await alovaInstance
      .Get(`${BASE_URL}/api/v1/settings/i18n/koko/?lang=${LanguageCode}&flat=0`)
      .then(response => (response as Response).json());

    for (const [key, value] of Object.entries(translations)) {
      mergeLocaleMessage(key, value);
    }
    nextTick(() => {
      loaded.value = true;
    });
  }
  catch (e) {
    throw new Error(`${e}`);
  }
});
</script>

<template>
  <n-config-provider
    :locale="enUS"
    :theme="darkTheme"
    :date-locale="dateZhCN"
    :theme-overrides="themeOverrides"
    class="flex items-center justify-center h-full w-full overflow-hidden"
  >
    <n-dialog-provider>
      <n-notification-provider>
        <n-message-provider>
          <router-view v-if="loaded" />
        </n-message-provider>
      </n-notification-provider>
    </n-dialog-provider>
  </n-config-provider>
</template>
