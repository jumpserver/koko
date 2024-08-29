<template>
    <n-config-provider :theme="darkTheme" :locale="zhCN" :date-locale="dateZhCN">
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
