<template>
    <Terminal v-if="verified" @event="onEvent" @ws-data="onWsData" />
</template>

<script setup lang="ts">
import { h, ref } from 'vue';
import { useI18n } from 'vue-i18n';
import {
    NInput,
    NButton,
    NGrid,
    NGridItem,
    useDialog,
    NForm,
    useMessage,
    useDialogReactiveList
} from 'naive-ui';

import Terminal from '@/components/Terminal/Terminal.vue';
import { useParamsStore } from '@/store/modules/params.ts';

const { t } = useI18n();
const dialog = useDialog();
const message = useMessage();
const dialogReactiveList = useDialogReactiveList();

const paramsStore = useParamsStore();

const verified = ref(false);
const verifyValue = ref('');
const terminalId = ref('');

const handleVerify = () => {
    if (verifyValue.value === '') return message.warning(t('InputVerifyCode'));

    dialogReactiveList.value.forEach(item => {
        if (item.class === 'verify') {
            verified.value = true;
            paramsStore.setShareCode(verifyValue.value);
            item.destroy();
        }
    });
};
const onEvent = () => {};
const onWsData = (msgType: string, msg: any, terminal: Terminal) => {
    switch (msgType) {
        case 'TERMINAL_SHARE_JOIN': {
            const data = JSON.parse(msg.data);
            const key = data.terminal_id;

            // if ()

            break;
        }
        case 'TERMINAL_SHARE_LEAVE': {
            break;
        }
        case 'TERMINAL_SHARE_USERS': {
            break;
        }
        case 'TERMINAL_RESIZE': {
            break;
        }
        case 'TERMINAL_SHARE_USER_REMOVE': {
            break;
        }
        case 'TERMINAL_SESSION': {
            break;
        }
        case 'TERMINAL_SESSION_PAUSE': {
            break;
        }
        case 'TERMINAL_SESSION_RESUME': {
            const data = JSON.parse(msg.data);

            message.info(`${data.user}: ${t('ResumeSession')}`);

            break;
        }
        default: {
            break;
        }
    }
};

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
                h(NGridItem, { span: 18, class: 'mr-[20px]' }, () =>
                    h(NInput, {
                        size: 'medium',
                        round: true,
                        showPasswordOn: 'mousedown',
                        value: verifyValue.value,
                        type: 'password',
                        'onUpdate:value': newValue => {
                            verifyValue.value = newValue;
                        }
                    })
                ),
                h(NGridItem, { span: 6 }, () =>
                    h(
                        NButton,
                        {
                            type: 'tertiary',
                            round: true,
                            class: 'w-full',
                            size: 'medium',
                            onClick: handleVerify
                        },
                        { default: () => t('ConfirmBtn') }
                    )
                )
            ])
        ]),
    onMaskClick: () => {
        message.warning(t('InputVerifyCode'));
    }
});
</script>
