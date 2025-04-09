<template>
  <n-drawer :show="show" resizable :default-width="502" @mask-click="closeDrawer" @update:show="closeDrawer">
    <!-- Settings 情况下的抽屉 -->
    <n-drawer-content :title="settings.drawerTitle" :native-scrollbar="false" closable>
      <template v-for="item of settings.items" :key="item.label">
        <n-form-item path="theme" :label-style="item.labelStyle" label-align="top">
          <template #label>
            <n-flex align="center" justify="space-between" class="w-full">
              <n-flex align="center" class="!gap-x-2">
                <component :is="item.labelIcon" size="14" />
                <span class="text-sm">{{ item.label }}</span>
              </n-flex>

              <n-tooltip size="small" v-if="item.showMore">
                <template #trigger>
                  <Ellipsis :size="14" class="cursor-pointer focus:outline-none" />
                </template>

                <span> show more </span>
              </n-tooltip>
            </n-flex>
          </template>
          <template v-if="item.type === 'select'">
            <n-select
              :value="currentTheme"
              :options="themeOptions"
              @keydown="previewTheme"
              @update:value="handleUpdateTheme"
              size="small"
            />
          </template>

          <template v-if="item.type === 'list'">
            <n-card size="small">
              <n-flex justify="center" vertical class="w-full">
                <n-flex align="center">
                  <n-text> 当前用户: </n-text>
                  <n-text depth="1" strong class="text-sm">{{ currentUser.user }}</n-text>
                </n-flex>

                <n-collapse @item-header-click="handleItemHeaderClick" :default-expanded-names="'online-user'">
                  <template #header-extra>
                    <ChevronLeft v-if="showLeftArrow" :size="18" class="focus:outline-none" />
                    <ChevronDown v-else="showLeftArrow" :size="18" class="focus:outline-none" />
                  </template>
                  <n-collapse-item title="在线用户:" name="online-user">
                    <n-flex
                      v-if="onlineUsers.length > 0"
                      v-for="item in onlineUsers"
                      :key="item.user_id"
                      align="center"
                      justify="space-between"
                      class="w-full"
                    >
                      {{ item.user }}

                      <n-popconfirm
                        :positive-text="t('Delete')"
                        :positive-button-props="{ type: 'error' }"
                        @positive-click="handlePositiveClick(item)"
                        @negative-click="handleNegativeClick"
                      >
                        <template #trigger>
                          <Delete
                            :size="20"
                            class="cursor-pointer focus:outline-none hover:text-red-500 hover:transition-all duration-300"
                          />
                        </template>
                        {{ t('RemoveShareUserConfirm') }}
                      </n-popconfirm>
                    </n-flex>
                    <n-empty v-else description="暂无在线用户" />
                  </n-collapse-item>
                </n-collapse>
              </n-flex>
            </n-card>
          </template>

          <template v-if="item.type === 'create'">
            <n-card size="small">
              <Share
                :share-id="shareId"
                :share-code="shareCode"
                :share-enable="shareEnable"
                :user-options="shareUserOptions"
              />
            </n-card>
          </template>

          <template v-if="item.type === 'keyboard'">
            <Keyboard />
          </template>
        </n-form-item>
      </template>
    </n-drawer-content>

    <!-- FileManager 情况下的抽屉 -->
  </n-drawer>
</template>

<script setup lang="ts">
import xtermTheme from 'xterm-theme';
import mittBus from '@/utils/mittBus.ts';
import Share from '@/components/Share/index.vue';
import Keyboard from '@/components/Keyboard/index.vue';

import { useI18n } from 'vue-i18n';
import { ref, watch, computed } from 'vue';
import { useTerminalSettingsStore } from '@/store/modules/terminalSettings';
import { Ellipsis, ChevronLeft, ChevronDown, Delete } from 'lucide-vue-next';

import type { SettingConfig } from '@/types/modules/setting.type';
import type { ShareUserOptions, OnlineUser } from '@/types/modules/user.type';

const props = defineProps<{
  shareId: string;
  shareCode: string;
  show: boolean;
  shareEnable: boolean;
  settings: SettingConfig;
  socketInstance: WebSocket | '';
  currentOnlineUsers: OnlineUser[];
  shareUserOptions: ShareUserOptions[];
}>();

const emit = defineEmits<{
  (e: 'update:open', value: boolean): void;
}>();

const { t } = useI18n();
const terminalSettingsStore = useTerminalSettingsStore();

const showLeftArrow = ref(false);
const currentTheme = ref(terminalSettingsStore.theme);
const themeOptions = ref([
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
]);

const currentUser = computed(() => {
  return props.currentOnlineUsers.filter(item => item.primary)[0];
});

const onlineUsers = computed(() => {
  return props.currentOnlineUsers.filter(item => !item.primary);
});

watch(
  () => props.currentOnlineUsers,
  value => {
    if (value.length > 0) {
      showLeftArrow.value = false;
    }

    showLeftArrow.value = true;
  }
);

watch(
  () => terminalSettingsStore.theme,
  value => {
    currentTheme.value = value;
  }
);

/**
 * @description 更新主题
 * @param value
 */
const handleUpdateTheme = (value: string) => {
  currentTheme.value = value;
  terminalSettingsStore.setDefaultTerminalConfig('theme', value);
};
/**
 * @description 预览主题
 * @param event
 */
const previewTheme = (event: KeyboardEvent) => {
  if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
    const currentIndex = themeOptions.value.findIndex(theme => theme.value === currentTheme.value);

    let nextIndex = currentIndex;

    if (event.key === 'ArrowUp') {
      // 如果当前索引为 0，则跳转到最后一个选项，否则向上移动
      nextIndex = currentIndex === 0 ? themeOptions.value.length - 1 : currentIndex - 1;
    } else if (event.key === 'ArrowDown') {
      // 如果当前索引为最后一个，则跳转到第一个选项，否则向下移动
      nextIndex = currentIndex === themeOptions.value.length - 1 ? 0 : currentIndex + 1;
    }

    currentTheme.value = themeOptions.value[nextIndex].value;
    terminalSettingsStore.setDefaultTerminalConfig('theme', themeOptions.value[nextIndex].value);
  }

  if (event.key === 'Enter') {
    handleUpdateTheme(currentTheme.value!);
  }
};
/**
 * @description 点击折叠按钮
 * @param data
 */
const handleItemHeaderClick = (data: { name: string | number; expanded: boolean; event: MouseEvent }) => {
  data.expanded ? (showLeftArrow.value = false) : (showLeftArrow.value = true);
};
/**
 * @description 移除在线用户
 * @param user_id
 */
const handlePositiveClick = (userMeta: OnlineUser) => {
  mittBus.emit('remove-share-user', {
    // todo 参数没有必要
    sessionId: '',
    userMeta: userMeta,
    type: 'TERMINAL_SHARE_USER_REMOVE'
  });
};
const handleNegativeClick = () => {};

/**
 * @description 关闭抽屉
 */
const closeDrawer = () => {
  emit('update:open', false);
};
</script>

<style scoped lang="scss">
.n-form-item.n-form-item--top-labelled .n-form-item-label {
  align-items: center;
  padding: unset;
}

.n-collapse {
  :deep(.n-collapse-item-arrow) {
    display: none !important;
  }

  :deep(.n-collapse-item__content-inner) {
    padding-top: 5px !important;
  }
}

:deep(.n-form-item-label__text) {
  width: 100%;
}
</style>
