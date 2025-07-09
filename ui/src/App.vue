<script setup lang="ts">
import type { GlobalThemeOverrides, NLocale } from 'naive-ui';

import { useI18n } from 'vue-i18n';
import { nextTick, onMounted, onUnmounted, ref } from 'vue';
import { darkTheme, dateZhCN, enUS, esAR, jaJP, koKR, ptBR, ruRU, zhCN, zhTW } from 'naive-ui';

import { alovaInstance } from '@/api';
import { useColor } from '@/hooks/useColor';
import { lunaCommunicator } from '@/utils/lunaBus';
import { LUNA_MESSAGE_TYPE } from '@/types/modules/message.type';
import { BASE_URL, LanguageCode, ThemeCode } from '@/utils/config';

import { createThemeOverrides } from './overrides';

const { mergeLocaleMessage } = useI18n();
const { setCurrentMainColor } = useColor();

const loaded = ref(false);
const componentsLocale = ref<NLocale | null>(null);
const themeOverrides = ref<GlobalThemeOverrides | null>(null);
const langCodeMap = new Map(
  Object.entries({
    ko: koKR,
    ru: ruRU,
    ja: jaJP,
    es: esAR,
    en: enUS,
    'pt-br': ptBR,
    'zh-hant': zhTW,
    'zh-hans': zhCN,
  })
);

const handleMainThemeChange = (themeName: any) => {
  console.log(themeName);
  setCurrentMainColor(themeName!.data as string);
  themeOverrides.value = createThemeOverrides(themeName!.data as 'default' | 'deepBlue' | 'darkGary');
};

onMounted(async () => {
  loaded.value = false;

  const langCode = langCodeMap.get(LanguageCode);

  setCurrentMainColor(ThemeCode);
  themeOverrides.value = createThemeOverrides(ThemeCode as 'default' | 'deepBlue' | 'darkGary');

  if (langCode) {
    componentsLocale.value = langCode;
  } else {
    componentsLocale.value = enUS;
  }

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
  } catch (e) {
    throw new Error(`${e}`);
  }

  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.CHANGE_MAIN_THEME, handleMainThemeChange);
});

onUnmounted(() => {
  lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.CHANGE_MAIN_THEME, handleMainThemeChange);
});
</script>

<template>
  <n-config-provider
    :theme="darkTheme"
    :date-locale="dateZhCN"
    :locale="componentsLocale"
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
