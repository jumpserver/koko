<template>
    <n-form label-placement="top">
        <n-grid :cols="24">
            <n-form-item-gi :span="24">
                <n-grid :cols="24">
                    <n-grid-item :span="20">
                        <n-select
                            v-model:value="theme"
                            :placeholder="t('SelectTheme')"
                            :options="themes"
                            @update:value="setTheme"
                            class="pr-[20px]"
                        />
                    </n-grid-item>
                    <n-grid-item :span="4">
                        <n-button :loading="loading" class="w-full" @click="syncTheme">
                            {{ t('Confirm') }}
                        </n-button>
                    </n-grid-item>
                </n-grid>
            </n-form-item-gi>
            <n-form-item-gi :span="24">
                <n-flex v-if="Object.keys(colors).length > 0">
                    <p class="title">Theme Colors</p>
                    <n-grid :cols="24" type="flex" class="theme-colors mb-[35px]">
                        <n-grid-item :span="8">
                            <div class="show-color" :style="{ backgroundColor: colors.background }"></div>
                            <div>Background</div>
                        </n-grid-item>
                        <n-grid-item :span="8">
                            <div class="show-color" :style="{ backgroundColor: colors.foreground }"></div>
                            <div>Foreground</div>
                        </n-grid-item>
                        <n-grid-item :span="8">
                            <div class="show-color" :style="{ backgroundColor: colors.cursor }"></div>
                            <div>Cursor</div>
                        </n-grid-item>
                    </n-grid>
                    <p class="title">ANSI Colors</p>
                    <n-grid :cols="24" type="flex" class="theme-colors">
                        <n-grid-item :span="3">
                            <div class="show-color" :style="{ backgroundColor: colors.black }"></div>
                            <div>Black</div>
                        </n-grid-item>
                        <n-grid-item :span="3">
                            <div class="show-color" :style="{ backgroundColor: colors.red }"></div>
                            <div>Red</div>
                        </n-grid-item>
                        <n-grid-item :span="3">
                            <div class="show-color" :style="{ backgroundColor: colors.green }"></div>
                            <div>Green</div>
                        </n-grid-item>
                        <n-grid-item :span="3">
                            <div class="show-color" :style="{ backgroundColor: colors.yellow }"></div>
                            <div>Yellow</div>
                        </n-grid-item>
                        <n-grid-item :span="3">
                            <div class="show-color" :style="{ backgroundColor: colors.blue }"></div>
                            <div>Blue</div>
                        </n-grid-item>
                        <n-grid-item :span="3">
                            <div class="show-color" :style="{ backgroundColor: colors.magenta }"></div>
                            <div>Magenta</div>
                        </n-grid-item>
                        <n-grid-item :span="3">
                            <div class="show-color" :style="{ backgroundColor: colors.cyan }"></div>
                            <div>Cyan</div>
                        </n-grid-item>
                        <n-grid-item :span="3">
                            <div class="show-color" :style="{ backgroundColor: colors.white }"></div>
                            <div>White</div>
                        </n-grid-item>
                    </n-grid>
                    <n-grid :cols="24" type="flex" class="theme-colors">
                        <n-grid-item :span="3">
                            <div class="show-color" :style="{ backgroundColor: colors.brightBlack }"></div>
                            <div>BrightBlack</div>
                        </n-grid-item>
                        <n-grid-item :span="3">
                            <div class="show-color" :style="{ backgroundColor: colors.brightRed }"></div>
                            <div>BrightRed</div>
                        </n-grid-item>
                        <n-grid-item :span="3">
                            <div class="show-color" :style="{ backgroundColor: colors.brightGreen }"></div>
                            <div>BrightGreen</div>
                        </n-grid-item>
                        <n-grid-item :span="3">
                            <div class="show-color" :style="{ backgroundColor: colors.brightYellow }"></div>
                            <div>BrightYellow</div>
                        </n-grid-item>
                        <n-grid-item :span="3">
                            <div class="show-color" :style="{ backgroundColor: colors.brightBlue }"></div>
                            <div>BrightBlue</div>
                        </n-grid-item>
                        <n-grid-item :span="3">
                            <div class="show-color" :style="{ backgroundColor: colors.brightMagenta }"></div>
                            <div>BrightMagenta</div>
                        </n-grid-item>
                        <n-grid-item :span="3">
                            <div class="show-color" :style="{ backgroundColor: colors.brightCyan }"></div>
                            <div>BrightCyan</div>
                        </n-grid-item>
                        <n-grid-item :span="3">
                            <div class="show-color" :style="{ backgroundColor: colors.brightWhite }"></div>
                            <div>BrightWhite</div>
                        </n-grid-item>
                    </n-grid>
                </n-flex>
            </n-form-item-gi>
        </n-grid>
    </n-form>
</template>

<script setup lang="ts">
import xtermTheme from 'xterm-theme';
import mittBus from '@/utils/mittBus.ts';

import { useI18n } from 'vue-i18n';
import { defaultTheme } from '@/config';
import { useDialogReactiveList } from 'naive-ui';
import { computed, onMounted, onUnmounted, ref } from 'vue';

const props = withDefaults(
    defineProps<{
        currentThemeName: string;
        preview: (tempTheme: string) => void;
    }>(),
    {
        currentThemeName: 'Default'
    }
);

const { t } = useI18n();

const loading = ref<boolean>(false);
const showThemeConfig = ref<boolean>(false);
const theme = ref<string>(props.currentThemeName);

const dialogReactiveList = useDialogReactiveList();

const themes = computed(() => {
    return [
        {
            label: 'Default',
            value: 'Default'
        },
        ...Object.keys(xtermTheme).map(item => {
            return {
                label: item,
                value: item
            };
        })
    ];
});
const colors = computed(() => {
    if (theme.value && Object.keys(xtermTheme).includes(theme.value)) {
        return xtermTheme[theme.value];
    } else {
        return defaultTheme;
    }
});

const setTheme = (value: string) => {
    theme.value = value;
    props.preview(theme.value);
    mittBus.emit('set-theme', { themeName: value });
};
const syncTheme = () => {
    loading.value = true;

    console.log('emits');
    mittBus.emit('sync-theme', {
        type: 'TERMINAL_SYNC_USER_PREFERENCE',
        data: { terminal_theme_name: theme.value }
    });

    setTimeout(() => {
        loading.value = false;
    }, 500);

    setTimeout(() => {
        dialogReactiveList.value.forEach(item => {
            if (item.class === 'set-theme') {
                item.destroy();
            }
        });
    }, 1000);
};

onMounted(() => {
    mittBus.on('show-theme-config', () => {
        showThemeConfig.value = !showThemeConfig.value;
    });
});
onUnmounted(() => {
    mittBus.off('show-theme-config');
});
</script>

<style scoped lang="scss">
.title {
    font-size: 14px;
    color: #fff;
}

.theme-colors {
    font-size: 12px;
    color: #fff;

    .show-color {
        width: 100%;
        height: 24px;
        margin-bottom: 10px;

        & ~ div {
            display: flex;
            justify-content: center;
        }
    }

    .n-col {
        text-align: center;
    }
}
</style>
