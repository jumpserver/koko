<template>
  <template v-if="!currentShareId">
    <n-form label-placement="top" :model="shareLinkRequest">
      <n-grid :cols="24">
        <n-form-item-gi :span="24" :label="t('ExpiredTime')">
          <n-select
            v-model:value="shareLinkRequest.expiredTime"
            size="small"
            :placeholder="t('SelectAction')"
            :options="expiredOptions"
          />
        </n-form-item-gi>
        <n-form-item-gi :span="24" :label="t('ActionPerm')">
          <n-select
            size="small"
            v-model:value="shareLinkRequest.actionPerm"
            :placeholder="t('ActionPerm')"
            :options="actionsPermOptions"
          />
        </n-form-item-gi>
        <n-form-item-gi :span="24" :label="t('ShareUser')">
          <n-select
            multiple
            filterable
            clearable
            remote
            size="small"
            v-model:value="shareLinkRequest.users"
            :loading="loading"
            :render-tag="renderTag"
            :options="mappedUserOptions"
            :clear-filter-after-select="false"
            :placeholder="t('GetShareUser')"
            @search="debounceSearch"
          />
        </n-form-item-gi>
      </n-grid>

      <n-button type="primary" size="small" class="!w-full" @click="handleShareURlCreated">
        <n-text class="text-white text-sm">
          {{ t('CreateLink') }}
        </n-text>
      </n-button>
    </n-form>
  </template>
  <template v-else>
    <n-result status="success" :description="t('CreateSuccess')" />

    <n-tooltip size="small">
      <template #trigger>
        <Undo2 :size="16" class="absolute top-2 right-2 focus:outline-none" cursor="pointer" @click="handleBack" />
      </template>

      <span>{{ t('Back') }}</span>
    </n-tooltip>

    <n-form label-placement="top">
      <n-grid :cols="24">
        <n-form-item-gi :label="t('LinkAddr')" :span="24">
          <n-input readonly :value="shareURL" />
        </n-form-item-gi>
        <n-form-item-gi :label="t('VerifyCode')" :span="24">
          <n-input readonly :value="shareCode"></n-input>
        </n-form-item-gi>
      </n-grid>

      <n-button type="primary" size="small" class="!w-full" @click="copyShareURL">
        <n-text class="text-white text-sm">
          {{ t('CopyLink') }}
        </n-text>
      </n-button>
    </n-form>
  </template>
</template>

<script setup lang="ts">
import mittBus from '@/utils/mittBus.ts';
import * as clipboard from 'clipboard-polyfill';

import { useI18n } from 'vue-i18n';
import { BASE_URL } from '@/config';
import { Undo2 } from 'lucide-vue-next';
import { getMinuteLabel } from '@/utils';
import { useMessage, NTag } from 'naive-ui';
import { useDebounceFn } from '@vueuse/core';
import { useDialogReactiveList } from 'naive-ui';
import { computed, nextTick, reactive, ref, watch, h } from 'vue';

import type { ShareUserOptions } from '@/types/modules/user.type';
import type { SelectRenderTag } from 'naive-ui';
import { useParamsStore } from '@/store/modules/params.ts';
// import { storeToRefs } from 'pinia';

const props = withDefaults(
  defineProps<{
    shareId: string;
    shareCode: string;
    sessionId?: string;
    shareEnable: boolean;
    userOptions: ShareUserOptions[];
  }>(),
  {
    shareId: '',
    shareCode: '',
    sessionId: '',
    shareEnable: false,
    userOptions: () => [
      {
        id: '',
        name: '',
        username: ''
      }
    ]
  }
);

const message = useMessage();
const paramsStore = useParamsStore();
const dialogReactiveList = useDialogReactiveList();

const { t } = useI18n();

// TODO k8s 中仍然是这种方式
// const { shareCode, shareId } = storeToRefs(paramsStore);

const currentShareId = ref<string>('');
const loading = ref<boolean>(false);
const currentEnableShare = ref<boolean>(false);
const shareUsers = ref<ShareUserOptions[]>([]);

const expiredOptions = reactive([
  { label: getMinuteLabel(1, t), value: 1 },
  { label: getMinuteLabel(5, t), value: 5 },
  { label: getMinuteLabel(10, t), value: 10 },
  { label: getMinuteLabel(20, t), value: 20 },
  { label: getMinuteLabel(60, t), value: 60 }
]);
const shareLinkRequest = reactive({
  expiredTime: 10,
  actionPerm: 'writable',
  users: [] as ShareUserOptions[]
});
const actionsPermOptions = reactive([
  { label: t('Writable'), value: 'writable' },
  { label: t('ReadOnly'), value: 'readonly' }
]);

const shareURL = computed(() => {
  return shareId.value ? `${BASE_URL}/luna/share/${shareId.value}/` : t('NoLink');
});
const mappedUserOptions = computed(() => {
  if (props.userOptions && props.userOptions.length > 0) {
    return props.userOptions.map((item: ShareUserOptions) => ({
      label: item.username,
      value: item.id
    }));
  } else {
    return [];
  }
});

watch(
  () => props.userOptions,
  newValue => {
    shareUsers.value = newValue;
    nextTick(() => {
      loading.value = false;
    });
  }
);
watch(
  () => props.shareId,
  id => {
    currentShareId.value = id;
  }
);
watch(
  () => props.shareEnable,
  enable => {
    currentEnableShare.value = enable;
  },
  { immediate: true }
);

/**
 * @description 返回创建表单
 */
const handleBack = () => {
  currentShareId.value = '';
};
/**
 * @description 复制分享链接
 */
const copyShareURL = () => {
  if (!currentShareId.value) return;
  if (!currentEnableShare.value) return;

  const url = shareURL.value;
  const linkTitle = t('LinkAddr');
  const codeTitle = t('VerifyCode');

  const text = `${linkTitle}: ${url}\n${codeTitle}: ${props.shareCode}`;

  clipboard
    .writeText(text)
    .then(() => {
      message.success(t('CopyShareURLSuccess'));
    })
    .catch(e => {
      message.error(`Copy Error for ${e}`);
    });

  dialogReactiveList.value.forEach(item => {
    if (item.class === 'share') {
      paramsStore.setShareId('');
      paramsStore.setShareCode('');
      item.destroy();
    }
  });
};
/**
 * @description 创建分享链接
 */
const handleShareURlCreated = () => {
  dialogReactiveList.value.forEach(item => {
    if (item.class === 'share') {
      item.title = t('Share');
    }
  });

  mittBus.emit('create-share-url', {
    type: 'TERMINAL_SHARE',
    sessionId: props.sessionId,
    shareLinkRequest: shareLinkRequest
  });
};
/**
 * @description 搜索分享用户
 */
const handleSearch = (query: string) => {
  loading.value = true;
  mittBus.emit('share-user', { type: 'TERMINAL_GET_SHARE_USER', query });
};
/**
 * @description 渲染分享用户标签
 */
const renderTag: SelectRenderTag = ({ option, handleClose }) => {
  return h(
    NTag,
    {
      closable: true,
      round: true,
      size: 'small',
      onMousedown: (e: FocusEvent) => {
        e.preventDefault();
      },
      onClose: (e: MouseEvent) => {
        e.stopPropagation();
        handleClose();
      }
    },
    {
      default: () => option.label
    }
  );
};

const debounceSearch = useDebounceFn(handleSearch, 300);
</script>
