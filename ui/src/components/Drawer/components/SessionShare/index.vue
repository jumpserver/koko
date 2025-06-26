<script setup lang="ts">
import type { SelectRenderTag } from 'naive-ui';

import { useI18n } from 'vue-i18n';
import { Delete } from 'lucide-vue-next';
import { useDebounceFn } from '@vueuse/core';
import { writeText } from 'clipboard-polyfill';
import { NTag, useDialogReactiveList, useMessage } from 'naive-ui';
import { computed, h, nextTick, onMounted, reactive, ref, watch } from 'vue';

import type { ShareUserOptions } from '@/types/modules/user.type';

import { getMinuteLabel } from '@/utils';
import { BASE_URL } from '@/utils/config';
import { useParamsStore } from '@/store/modules/params.ts';
import { useConnectionStore } from '@/store/modules/useConnection.ts';

const props = withDefaults(
  defineProps<{
    shareId?: string;
    shareCode?: string;
    sessionId?: string;
    shareEnable?: boolean;
    userOptions?: ShareUserOptions[];
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
        username: '',
      },
    ],
  }
);

const emits = defineEmits<{
  (e: 'createShareUrl', shareLinkRequest: any): void;
  (e: 'searchShareUser', query: string): void;
}>();

const message = useMessage();
const paramsStore = useParamsStore();
const dialogReactiveList = useDialogReactiveList();

const { t } = useI18n();

const currentShareId = ref<string>('');
const loading = ref<boolean>(false);
const currentEnableShare = ref<boolean>(false);
const shareUsers = ref<ShareUserOptions[]>([]);

const expiredOptions = reactive([
  { label: getMinuteLabel(1, t), value: 1 },
  { label: getMinuteLabel(5, t), value: 5 },
  { label: getMinuteLabel(10, t), value: 10 },
  { label: getMinuteLabel(20, t), value: 20 },
  { label: getMinuteLabel(60, t), value: 60 },
]);
const shareLinkRequest = reactive({
  expiredTime: 10,
  actionPerm: 'writable',
  users: [] as ShareUserOptions[],
});
const actionsPermOptions = reactive([
  { label: t('Writable'), value: 'writable' },
  { label: t('ReadOnly'), value: 'readonly' },
]);

const shareURL = computed(() => {
  return props.shareId ? `${BASE_URL}/luna/share/${props.shareId}/` : t('NoLink');
});
const mappedUserOptions = computed(() => {
  if (props.userOptions && props.userOptions.length > 0) {
    return props.userOptions.map((item: ShareUserOptions) => ({
      label: item.username,
      value: item.id,
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
function handleBack() {
  currentShareId.value = '';
}
/**
 * @description 复制分享链接
 */
function copyShareURL() {
  if (!currentShareId.value) return;
  if (!currentEnableShare.value) return;

  const url = shareURL.value;
  const linkTitle = t('LinkAddr');
  const codeTitle = t('VerifyCode');

  const text = `${linkTitle}: ${url}\n${codeTitle}: ${props.shareCode}`;

  writeText(text)
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
}
/**
 * @description 创建分享链接
 */
function handleShareURlCreated() {
  dialogReactiveList.value.forEach(item => {
    if (item.class === 'share') {
      item.title = t('Share');
    }
  });

  emits('createShareUrl', shareLinkRequest);
}
/**
 * @description 搜索分享用户
 */
function handleSearch(query: string) {
  loading.value = true;

  emits('searchShareUser', query);
}
/**
 * @description 渲染分享用户标签
 */
const renderTag: SelectRenderTag = ({ option, handleClose }) => {
  return h(
    NTag,
    {
      closable: true,
      size: 'small',
      type: 'primary',
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
    }
  );
};

const debounceSearch = useDebounceFn(handleSearch, 300);

onMounted(() => {
  const connectionStore = useConnectionStore();
  console.log(connectionStore);
});
</script>

<template>
  <n-flex vertical align="center">
    <n-divider dashed title-placement="left" class="!mb-3 !mt-0">
      <n-text depth="2" class="text-sm opacity-70"> 在线用户（{{ shareUsers.length }}） </n-text>
    </n-divider>

    <n-flex class="w-full">
      <n-list class="w-full" bordered hoverable>
        <n-list-item>
          <template #suffix>
            <Delete :size="18" class="cursor-pointer hover:text-red-500 transition-all duration-200" />
          </template>

          <n-flex vertical>
            <n-text>Admin</n-text>

            <NTag :bordered="false" type="primary"> 主用户 </NTag>
          </n-flex>
        </n-list-item>
      </n-list>
    </n-flex>
  </n-flex>
  <!-- <T ransition name="fade" mode="out-in">
    <div v-if="!currentShareId" key="create-form" class="min-h-[305px]">
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
              v-model:value="shareLinkRequest.actionPerm"
              size="small"
              :placeholder="t('ActionPerm')"
              :options="actionsPermOptions"
            />
          </n-form-item-gi>
          <n-form-item-gi :span="24" :label="t('ShareUser')">
            <n-select
              v-model:value="shareLinkRequest.users"
              multiple
              filterable
              clearable
              remote
              size="small"
              :loading="loading"
              :render-tag="renderTag"
              :options="mappedUserOptions"
              :clear-filter-after-select="false"
              :placeholder="t('GetShareUser')"
              @search="debounceSearch"
            />
          </n-form-item-gi>
        </n-grid>

        <n-button
          type="primary"
          size="small"
          class="!w-full mt-1"
          :disabled="!currentEnableShare"
          @click="handleShareURlCreated"
        >
          <n-text class="text-white text-sm">
            {{ t('CreateLink') }}
          </n-text>
        </n-button>
      </n-form>
    </div>
    <div v-else key="share-result" class="relative min-h-[305px]">
      <n-result status="success" :description="t('CreateSuccess')" class="relative" />

      <n-tooltip size="small">
        <template #trigger>
          <Undo2 :size="16" class="absolute top-0 right-0 focus:outline-none" cursor="pointer" @click="handleBack" />
        </template>

        <span>{{ t('Back') }}</span>
      </n-tooltip>

      <n-form label-placement="top">
        <n-grid :cols="24">
          <n-form-item-gi :label="t('LinkAddr')" :span="24">
            <n-input readonly :value="shareURL" />
          </n-form-item-gi>
          <n-form-item-gi :label="t('VerifyCode')" :span="24">
            <n-input readonly :value="shareCode" />
          </n-form-item-gi>
        </n-grid>

        <n-button type="primary" size="small" class="!w-full" @click="copyShareURL">
          <n-text class="text-white text-sm">
            {{ t('CopyLink') }}
          </n-text>
        </n-button>
      </n-form>
    </div>
  </T> -->
</template>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition:
    opacity 0.5s ease,
    transform 0.5s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
  transform: translateY(10px);
}
</style>
