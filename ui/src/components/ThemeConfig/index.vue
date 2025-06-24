<script setup lang="ts">
import { useDialogReactiveList } from 'naive-ui';
import { computed, onMounted, onUnmounted, ref } from 'vue';

import { useI18n } from 'vue-i18n';
import xtermTheme from 'xterm-theme';
import { defaultTheme } from '@/utils/config';
import mittBus from '@/utils/mittBus';

const props = withDefaults(
  defineProps<{
    currentThemeName?: string;
    preview: (tempTheme: string) => void;
  }>(),
  {
    currentThemeName: 'Default',
  },
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
      value: 'Default',
    },
    ...Object.keys(xtermTheme).map((item) => {
      return {
        label: item,
        value: item,
      };
    }),
  ];
});
const colors = computed(() => {
  if (theme.value && Object.keys(xtermTheme).includes(theme.value)) {
    return xtermTheme[theme.value];
  }
  else {
    return defaultTheme;
  }
});

/**
 * 设置主题
 *
 * @param value
 */
function setTheme(value: string) {
  theme.value = value;
  props.preview(theme.value);
  mittBus.emit('set-theme', { themeName: value });
}

/**
 * 处理当使用键盘上下键时的主题预览功能
 *
 * @param event
 */
function handlePreviewTheme(event: KeyboardEvent) {
  if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
    const currentIndex = themes.value.findIndex(option => option.value === theme.value);

    let nextIndex = currentIndex;

    if (event.key === 'ArrowUp') {
      // 如果当前索引为 0，则跳转到最后一个选项，否则向上移动
      nextIndex = currentIndex === 0 ? themes.value.length - 1 : currentIndex - 1;
    }
    else if (event.key === 'ArrowDown') {
      // 如果当前索引为最后一个，则跳转到第一个选项，否则向下移动
      nextIndex = currentIndex === themes.value.length - 1 ? 0 : currentIndex + 1;
    }

    const nextValue = themes.value[nextIndex]?.value;

    if (nextValue) {
      setTheme(nextValue);

      setTimeout(() => {
        const el = document.getElementsByClassName('n-base-select-option--selected')[0] as HTMLElement;

        el.classList.add('n-base-select-option--pending');
      }, 100);
    }
  }
}

/**
 * 点击同步按钮的回调
 */
function syncTheme() {
  loading.value = true;

  mittBus.emit('sync-theme', {
    type: 'TERMINAL_SYNC_USER_PREFERENCE',
    data: { terminal_theme_name: theme.value },
  });

  setTimeout(() => {
    loading.value = false;
  }, 500);

  setTimeout(() => {
    dialogReactiveList.value.forEach((item) => {
      if (item.class === 'set-theme') {
        item.destroy();
      }
    });
  }, 1000);
}

onMounted(() => {
  mittBus.on('show-theme-config', () => {
    showThemeConfig.value = !showThemeConfig.value;
  });
});

onUnmounted(() => {
  mittBus.off('show-theme-config');
});
</script>

<template>
  <n-form label-placement="top">
    <n-grid :cols="24">
      <n-form-item-gi :span="24">
        <n-grid :cols="24">
          <n-grid-item :span="20">
            <n-select
              v-model:value="theme"
              class="custom-select pr-[20px]"
              :options="themes"
              :placeholder="t('SelectTheme')"
              @update:value="setTheme"
              @keydown="handlePreviewTheme"
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
          <p class="title">
            Theme Colors
          </p>
          <n-grid :cols="24" type="flex" class="theme-colors mb-[35px]">
            <n-grid-item :span="8">
              <div class="show-color" :style="{ backgroundColor: colors.background }" />
              <div>Background</div>
            </n-grid-item>
            <n-grid-item :span="8">
              <div class="show-color" :style="{ backgroundColor: colors.foreground }" />
              <div>Foreground</div>
            </n-grid-item>
            <n-grid-item :span="8">
              <div class="show-color" :style="{ backgroundColor: colors.cursor }" />
              <div>Cursor</div>
            </n-grid-item>
          </n-grid>
          <p class="title">
            ANSI Colors
          </p>
          <n-grid :cols="24" type="flex" class="theme-colors">
            <n-grid-item :span="3">
              <div class="show-color" :style="{ backgroundColor: colors.black }" />
              <div>Black</div>
            </n-grid-item>
            <n-grid-item :span="3">
              <div class="show-color" :style="{ backgroundColor: colors.red }" />
              <div>Red</div>
            </n-grid-item>
            <n-grid-item :span="3">
              <div class="show-color" :style="{ backgroundColor: colors.green }" />
              <div>Green</div>
            </n-grid-item>
            <n-grid-item :span="3">
              <div class="show-color" :style="{ backgroundColor: colors.yellow }" />
              <div>Yellow</div>
            </n-grid-item>
            <n-grid-item :span="3">
              <div class="show-color" :style="{ backgroundColor: colors.blue }" />
              <div>Blue</div>
            </n-grid-item>
            <n-grid-item :span="3">
              <div class="show-color" :style="{ backgroundColor: colors.magenta }" />
              <div>Magenta</div>
            </n-grid-item>
            <n-grid-item :span="3">
              <div class="show-color" :style="{ backgroundColor: colors.cyan }" />
              <div>Cyan</div>
            </n-grid-item>
            <n-grid-item :span="3">
              <div class="show-color" :style="{ backgroundColor: colors.white }" />
              <div>White</div>
            </n-grid-item>
          </n-grid>
          <n-grid :cols="24" type="flex" class="theme-colors">
            <n-grid-item :span="3">
              <div class="show-color" :style="{ backgroundColor: colors.brightBlack }" />
              <div>BrightBlack</div>
            </n-grid-item>
            <n-grid-item :span="3">
              <div class="show-color" :style="{ backgroundColor: colors.brightRed }" />
              <div>BrightRed</div>
            </n-grid-item>
            <n-grid-item :span="3">
              <div class="show-color" :style="{ backgroundColor: colors.brightGreen }" />
              <div>BrightGreen</div>
            </n-grid-item>
            <n-grid-item :span="3">
              <div class="show-color" :style="{ backgroundColor: colors.brightYellow }" />
              <div>BrightYellow</div>
            </n-grid-item>
            <n-grid-item :span="3">
              <div class="show-color" :style="{ backgroundColor: colors.brightBlue }" />
              <div>BrightBlue</div>
            </n-grid-item>
            <n-grid-item :span="3">
              <div class="show-color" :style="{ backgroundColor: colors.brightMagenta }" />
              <div>BrightMagenta</div>
            </n-grid-item>
            <n-grid-item :span="3">
              <div class="show-color" :style="{ backgroundColor: colors.brightCyan }" />
              <div>BrightCyan</div>
            </n-grid-item>
            <n-grid-item :span="3">
              <div class="show-color" :style="{ backgroundColor: colors.brightWhite }" />
              <div>BrightWhite</div>
            </n-grid-item>
          </n-grid>
        </n-flex>
      </n-form-item-gi>
    </n-grid>
  </n-form>
</template>

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

.custom-select {
}
</style>
