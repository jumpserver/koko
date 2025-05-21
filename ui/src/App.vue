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
          <router-view v-if="loaded" />
        </n-message-provider>
      </n-notification-provider>
    </n-dialog-provider>
  </n-config-provider>
</template>

<script setup lang="ts">
import { lang } from '@/config';
import { useI18n } from 'vue-i18n';
import { BASE_URL } from '@/config';
import { darkTheme } from 'naive-ui';
import { alovaInstance } from '@/api';
import { zhCN, dateZhCN } from 'naive-ui';
import { onMounted, ref, nextTick } from 'vue';
import { themeOverrides } from './overrides.ts';

const { mergeLocaleMessage } = useI18n();

const loaded = ref(false);

onMounted(async () => {
  loaded.value = false;
  try {
    const translations = await alovaInstance
      .Get(`${BASE_URL}/api/v1/settings/i18n/koko/?lang=${lang}&flat=0`)
      .then(response => (response as Response).json());

    if (translations[lang]) {
      mergeLocaleMessage(lang, translations[lang]);

      nextTick(() => {
        loaded.value = true;
      });
    } else {
      const defaultTranslations = Reflect.ownKeys(translations)[0] as string;

      mergeLocaleMessage(defaultTranslations, translations[defaultTranslations]);

      nextTick(() => {
        loaded.value = true;
      });
    }
  } catch (e) {
    throw new Error(`${e}`);
  }
});
</script>
