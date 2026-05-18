<script setup lang="ts">
import type { GlobalThemeOverrides, NLocale } from 'naive-ui';

import { useI18n } from 'vue-i18n';
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue';
import { darkTheme, dateZhCN, enUS, esAR, jaJP, koKR, ptBR, ruRU, zhCN, zhTW } from 'naive-ui';

import type { SettingConfig } from '@/types/modules/config.type';

import { alovaInstance } from '@/api';
import { useColor } from '@/hooks/useColor';
import { lunaCommunicator } from '@/utils/lunaBus';
import { useGlobalStore } from '@/store/modules/global';
import { LUNA_MESSAGE_TYPE } from '@/types/modules/message.type';
import { BASE_URL, ColorMode, LanguageCode, ThemeCode } from '@/utils/config';

import { createThemeOverrides } from './overrides';

const { mergeLocaleMessage } = useI18n();
const { setCurrentMainColor } = useColor();
const globalStore = useGlobalStore();

const loaded = ref(false);
const componentsLocale = ref<NLocale | null>(null);
const themeOverrides = ref<GlobalThemeOverrides | null>(null);
const appTheme = computed(() => (ColorMode === 'dark' ? darkTheme : null));
const langCodeMap = new Map(
  Object.entries({
    'ko': koKR,
    'ru': ruRU,
    'ja': jaJP,
    'es': esAR,
    'en': enUS,
    'pt-br': ptBR,
    'zh-hant': zhTW,
    'zh-hans': zhCN,
  }),
);

const handleMainThemeChange = (themeName: any) => {
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
  }
  else {
    componentsLocale.value = enUS;
  }

  const [translationsResult, publicSettingsResult] = await Promise.allSettled([
    alovaInstance.Get(`${BASE_URL}/api/v1/settings/i18n/koko/?lang=${LanguageCode}&flat=0`).then(response => (response as Response).json()),
    alovaInstance.Get(`${BASE_URL}/api/v1/settings/public/`).then(response => (response as Response).json() as Promise<SettingConfig>),
  ]);

  if (translationsResult.status === 'fulfilled') {
    for (const [key, value] of Object.entries(translationsResult.value)) {
      mergeLocaleMessage(key, value);
    }
  }
  else {
    console.error('Failed to load koko i18n settings:', translationsResult.reason);
  }

  if (publicSettingsResult.status === 'fulfilled') {
    globalStore.setInterfaceVendor(publicSettingsResult.value.INTERFACE?.vender?.toLowerCase() ?? null);
  }
  else {
    console.error('Failed to load public settings:', publicSettingsResult.reason);
  }

  await nextTick();
  loaded.value = true;

  lunaCommunicator.onLuna(LUNA_MESSAGE_TYPE.CHANGE_MAIN_THEME, handleMainThemeChange);
});

onUnmounted(() => {
  lunaCommunicator.offLuna(LUNA_MESSAGE_TYPE.CHANGE_MAIN_THEME, handleMainThemeChange);
});
</script>

<template>
  <n-config-provider
    :theme="appTheme"
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
