<script setup lang="ts">
import type { SelectRenderTag } from 'naive-ui';

import { useI18n } from 'vue-i18n';
import { NTag, useMessage } from 'naive-ui';
import { useDebounceFn } from '@vueuse/core';
import { ArrowLeft, Copy, Link } from 'lucide-vue-next';
import { computed, h, reactive, ref, watch } from 'vue';

import type { ShareUserOptions } from '@/types/modules/user.type';

import { getMinuteLabel } from '@/utils';
import { useColor } from '@/hooks/useColor';
import { useSessionAdapter } from '@/hooks/useSessionAdapter';

interface ExpiredOption {
  label: string;
  value: number;
  checked: boolean;
}

interface ActionPermOption {
  label: string;
  value: string;
  checked: boolean;
}

defineProps<{
  disabledCreateLink: boolean;
}>();

const { t } = useI18n();
const { lighten } = useColor();
const message = useMessage();
const {
  shareInfo,
  userOptions,
  createShareLink,
  searchUsers,
  resetShareState,
  copyShareURL: _adapterCopyShareURL,
} = useSessionAdapter();

const searchLoading = ref<boolean>(false);
const showLinkResult = ref<boolean>(false);

watch(
  () => userOptions.value,
  (userOptions: ShareUserOptions[]) => {
    if (userOptions && userOptions.length > 0) {
      searchLoading.value = false;
    }
  },
);

watch(
  () => shareInfo.value.shareCode,
  (nv) => {
    if (nv) {
      showLinkResult.value = true;
    }
    else {
      showLinkResult.value = false;
    }
  },
);

const mappedUserOptions = computed(() => {
  if (userOptions.value && userOptions.value.length > 0) {
    return userOptions.value.map((item: ShareUserOptions) => ({
      label: item.username,
      value: item.id,
    }));
  }
  else {
    return [];
  }
});

// 装饰器模式：创建单选处理器
const createSingleSelectHandler = <T, K extends keyof T>(
  options: T[],
  valueKey: K,
  checkedKey: keyof T,
  onSelect?: (value: T[K]) => void,
) => {
  return (selectedValue: T[K]) => {
    options.forEach((item) => {
      (item as any)[checkedKey] = item[valueKey] === selectedValue;
    });

    // 执行回调函数，更新 shareLinkRequest
    if (onSelect) {
      onSelect(selectedValue);
    }
  };
};

const shareLinkRequest = reactive({
  expiredTime: 10,
  actionPerm: 'writable',
  users: [] as ShareUserOptions[],
});

const expiredOptions = reactive<ExpiredOption[]>([
  { label: getMinuteLabel(1, t), value: 1, checked: false },
  { label: getMinuteLabel(5, t), value: 5, checked: false },
  { label: getMinuteLabel(10, t), value: 10, checked: true },
  { label: getMinuteLabel(20, t), value: 20, checked: false },
  { label: getMinuteLabel(60, t), value: 60, checked: false },
]);

const actionsPermOptions = reactive<ActionPermOption[]>([
  { label: t('Writable'), value: 'writable', checked: true },
  { label: t('ReadOnly'), value: 'readonly', checked: false },
]);

const renderTag: SelectRenderTag = ({ option, handleClose }) => {
  return h(
    NTag,
    {
      closable: true,
      size: 'small',
      type: 'info',
      bordered: false,
      onMousedown: (e: FocusEvent) => {
        e.preventDefault();
      },
      onClose: (e: MouseEvent) => {
        e.stopPropagation();
        handleClose();
      },
    },
    {
      default: () => option.label,
    },
  );
};

const handleSearch = (query: string) => {
  searchLoading.value = true;
  searchUsers(query);
};

const debounceSearch = useDebounceFn(handleSearch, 300);
const handleChangeExpired = createSingleSelectHandler(expiredOptions, 'value', 'checked', (value) => {
  shareLinkRequest.expiredTime = value;
});
const handleChangeActionPerm = createSingleSelectHandler(actionsPermOptions, 'value', 'checked', (value) => {
  shareLinkRequest.actionPerm = value;
});

/**
 * @description 创建会话分享链接
 */
const handleCreateLink = () => {
  if (!shareInfo.value.sessionId) {
    return message.error(t('FailedCreateConnection'));
  }

  createShareLink(shareLinkRequest);
};

/**
 * @description 复制会话分享链接
 */
const handleCopyShareURL = () => {
  _adapterCopyShareURL();
};

/**
 * @description 返回到上一层
 */
const handleBack = () => {
  resetShareState();
};
</script>

<template>
  <n-descriptions v-if="!showLinkResult" label-placement="top" :column="1">
    <n-descriptions-item>
      <template #label>
        <n-text class="text-xs-plus" depth="1">
          会话持续时间
        </n-text>
      </template>

      <n-flex align="center" class="mt-2 cursor-pointer">
        <n-card
          v-for="item in expiredOptions"
          :key="item.value"
          bordered
          hoverable
          size="small"
          :content-style="{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
          }"
          :style="{
            border: item.checked ? `1px solid ${lighten(20)}` : '',
          }"
          style="width: 110px; height: 45px"
          @click="handleChangeExpired(item.value)"
        >
          <n-text depth="2" class="text-xs-plus">
            {{ item.label }}
          </n-text>
        </n-card>
      </n-flex>
    </n-descriptions-item>

    <n-descriptions-item>
      <n-divider dashed class="!my-1" />
    </n-descriptions-item>

    <n-descriptions-item>
      <template #label>
        <n-text class="text-xs-plus" depth="1">
          权限设置
        </n-text>
      </template>

      <n-flex align="center" :wrap="false" class="mt-2 cursor-pointer">
        <n-card
          v-for="item in actionsPermOptions"
          :key="item.value"
          bordered
          hoverable
          size="small"
          :content-style="{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
          }"
          :style="{
            border: item.checked ? `1px solid ${lighten(20)}` : '',
          }"
          style="width: 50%"
          @click="handleChangeActionPerm(item.value)"
        >
          <n-text depth="1" class="text-xs-plus">
            {{ item.label }}
          </n-text>
        </n-card>
      </n-flex>
    </n-descriptions-item>

    <n-descriptions-item>
      <n-divider dashed class="!my-1" />
    </n-descriptions-item>

    <n-descriptions-item>
      <template #label>
        <n-text class="text-xs-plus">
          参与者
        </n-text>
      </template>

      <n-flex vertical class="mt-2">
        <n-select
          v-model:value="shareLinkRequest.users"
          multiple
          filterable
          clearable
          remote
          size="small"
          :loading="searchLoading"
          :render-tag="renderTag"
          :options="mappedUserOptions"
          :clear-filter-after-select="false"
          :placeholder="t('GetShareUser')"
          @search="debounceSearch"
          @focus="debounceSearch('')"
        />
      </n-flex>
    </n-descriptions-item>

    <n-descriptions-item>
      <n-divider class="!my-1" />
    </n-descriptions-item>

    <n-descriptions-item>
      <n-button block secondary type="primary" class="mt-2 !text-xs-plus" :disabled="disabledCreateLink" @click="handleCreateLink">
        {{ t('CreateLink') }}
      </n-button>
    </n-descriptions-item>
  </n-descriptions>

  <n-descriptions v-else label-placement="top" :column="1">
    <n-descriptions-item>
      <n-input placeholder="搜索" round size="small" readonly :value="shareInfo.shareURL">
        <template #prefix>
          <Link :size="14" />
        </template>
      </n-input>

      <n-card
        size="small"
        :content-style="{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
        }"
        class="mt-4"
      >
        <n-text>
          {{ t('VerifyCode') }}
        </n-text>

        <n-text depth="2" class="text-2xl tracking-widest">
          {{ shareInfo.shareCode }}
        </n-text>
      </n-card>

      <n-flex align="center" :wrap="false" class="w-full mt-4">
        <n-button secondary type="success" class="!w-1/2" @click="handleCopyShareURL">
          <template #icon>
            <Copy />
          </template>

          {{ t('CopyLink') }}
        </n-button>
        <n-button secondary class="!w-1/2" @click="handleBack">
          <template #icon>
            <ArrowLeft />
          </template>

          {{ t('Back') }}
        </n-button>
      </n-flex>
    </n-descriptions-item>
  </n-descriptions>
</template>
