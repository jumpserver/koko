<template></template>

<script setup lang="ts">
import { h } from 'vue';
import { useI18n } from 'vue-i18n';
import { NInput, NButton, NGrid, NGridItem, useDialog, NForm, useMessage } from 'naive-ui';

const { t } = useI18n();
const dialog = useDialog();
const message = useMessage();

dialog.warning({
  class: 'verify',
  title: t('VerifyCode'),
  showIcon: false,
  maskClosable: false,
  style: 'width: 50%; padding-bottom: 45px',
  titleStyle: 'margin-bottom: 30px',
  content: () =>
    h(NForm, {}, () => [
      h(NGrid, {}, () => [
        h(NGridItem, { span: 20, class: 'mr-[20px]' }, () => h(NInput, { size: 'medium' })),
        h(NGridItem, { span: 4 }, () =>
          h(
            NButton,
            { type: 'tertiary', round: true, class: 'w-full', size: 'medium' },
            { default: () => t('ConfirmBtn') }
          )
        )
      ])
    ]),
  onMaskClick: () => {
    message.success('不能关闭');
  }
});
</script>

<style scoped></style>
