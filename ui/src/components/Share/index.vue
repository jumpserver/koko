<template>
  <template v-if="!shareId">
    <n-form label-placement="top" :model="shareLinkRequest">
      <n-grid :cols="24">
        <n-form-item-gi :span="24" :label="t('ExpiredTime')">
          <n-select
            v-model:value="shareLinkRequest.expiredTime"
            :placeholder="t('SelectAction')"
            :options="expiredOptions"
          />
        </n-form-item-gi>
        <n-form-item-gi :span="24" :label="t('ActionPerm')">
          <n-select
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
            v-model:value="shareLinkRequest.users"
            :loading="loading"
            :render-tag="renderTag"
            :options="mappedUserOptions"
            :clear-filter-after-select="false"
            :placeholder="t('GetShareUser')"
            @search="debounceSearch"
          />
        </n-form-item-gi>
        <n-form-item-gi :span="24">
          <n-button round tertiary type="primary" class="w-full text-white" @click="handleShareURlCreated">
            {{ t('CreateLink') }}
          </n-button>
        </n-form-item-gi>
      </n-grid>
    </n-form>
  </template>
  <template v-else>
    <n-result status="success" :description="t('CreateSuccess')" />

    <n-form label-placement="top">
      <n-grid :cols="24">
        <n-form-item-gi :label="t('LinkAddr')" :span="24">
          <n-input readonly :value="shareURL" />
        </n-form-item-gi>
        <n-form-item-gi :label="t('VerifyCode')" :span="24">
          <n-input readonly :value="shareCode"></n-input>
        </n-form-item-gi>
        <n-form-item-gi :span="24">
          <n-button round tertiary type="primary" class="w-full text-white" @click="copyShareURL">
            {{ t('CopyLink') }}
          </n-button>
        </n-form-item-gi>
      </n-grid>
    </n-form>
  </template>
</template>

<script setup lang="ts">
import mittBus from '@/utils/mittBus.ts';
import * as clipboard from 'clipboard-polyfill';

import { useMessage, NTag } from 'naive-ui';

import { useI18n } from 'vue-i18n';
import { BASE_URL } from '@/config';
import { getMinuteLabel } from '@/utils';
import { useDebounceFn } from '@vueuse/core';
import { shareUser } from '@/types';
import { useDialogReactiveList } from 'naive-ui';
import { computed, nextTick, reactive, ref, watch, h } from 'vue';

import type { SelectRenderTag } from 'naive-ui';
import { useParamsStore } from '@/store/modules/params.ts';
import { storeToRefs } from 'pinia';

const props = withDefaults(
  defineProps<{
    sessionId: string;
    enableShare: boolean;
    userOptions: shareUser[];
  }>(),
  {
    sessionId: '',
    enableShare: false,
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
const { shareCode, shareId } = storeToRefs(paramsStore);

const loading = ref<boolean>(false);
const shareUsers = ref<shareUser[]>([]);

const expiredOptions = reactive([
  { label: getMinuteLabel(1, t), value: 1 },
  { label: getMinuteLabel(5, t), value: 5 },
  { label: getMinuteLabel(10, t), value: 10 },
  { label: getMinuteLabel(20, t), value: 20 },
  { label: getMinuteLabel(60, t), value: 60 }
]);
const actionsPermOptions = reactive([
  { label: t('Writable'), value: 'writable' },
  { label: t('ReadOnly'), value: 'readonly' }
]);
const shareLinkRequest = reactive({
  expiredTime: 10,
  actionPerm: 'writable',
  users: [] as shareUser[]
});

const shareURL = computed(() => {
  return shareId.value ? `${BASE_URL}/koko/share/${shareId.value}/` : t('NoLink');
});

const mappedUserOptions = computed(() => {
  if (props.userOptions && props.userOptions.length > 0) {
    return props.userOptions.map((item: shareUser) => ({
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

const copyShareURL = () => {
  if (!shareId.value) return;
  if (!props.enableShare) return;

  const url = shareURL.value;
  const linkTitle = t('LinkAddr');
  const codeTitle = t('VerifyCode');

  const text = `${linkTitle}: ${url}\n${codeTitle}: ${shareCode.value}`;

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
const handleSearch = (query: string) => {
  loading.value = true;
  mittBus.emit('share-user', { type: 'TERMINAL_GET_SHARE_USER', query });
};
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
