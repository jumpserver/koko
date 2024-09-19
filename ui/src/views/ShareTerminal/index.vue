<template>
    <n-watermark
        cross
        selectable
        :rotate="-15"
        :font-size="16"
        :width="192"
        :height="128"
        :x-offset="12"
        :y-offset="28"
        :content="waterMarkContent"
        :line-height="16"
    >
        <CustomTerminal
            v-if="verified"
            class="common-terminal"
            index-key="id"
            terminal-type="common"
            @socketData="onSocketData"
        />
    </n-watermark>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { h, reactive, ref } from 'vue';
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

import CustomTerminal from '@/components/CustomTerminal/index.vue';

import { storeToRefs } from 'pinia';
import { useLogger } from '@/hooks/useLogger.ts';
import { useParamsStore } from '@/store/modules/params.ts';

import { Terminal } from '@xterm/xterm';

const { t } = useI18n();
const { info } = useLogger('Share CustomTerminal Component');
const dialog = useDialog();
const message = useMessage();
const dialogReactiveList = useDialogReactiveList();

const paramsStore = useParamsStore();

const verified = ref(false);
const terminalId = ref('');
const verifyValue = ref('');
const waterMarkContent = ref('');

const onlineUsersMap = reactive<{ [key: string]: any }>({});

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

const onSocketData = (msgType: string, msg: any, _terminal: Terminal) => {
    switch (msgType) {
        case 'TERMINAL_SHARE_JOIN': {
            const data = JSON.parse(msg.data);
            const key: string = data.terminal_id;

            onlineUsersMap[key] = data;

            info(onlineUsersMap);

            if (terminalId.value === key) {
                message.success(`${data.user} ${t('JoinedWithSuccess')}`);
                break;
            }

            message.success(`${data.user} ${t('JoinShare')}`);

            break;
        }
        case 'TERMINAL_SHARE_LEAVE': {
            const data = JSON.parse(msg.data);
            const key = data.terminal_id;

            if (onlineUsersMap.hasOwnProperty(key)) {
                delete onlineUsersMap[key];
            }

            message.info(`${data.user} ${t('LeaveShare')}`);
            break;
        }
        case 'TERMINAL_SHARE_USERS': {
            const data = JSON.parse(msg.data);

            info(data);

            Object.assign(onlineUsersMap, data);

            break;
        }
        case 'TERMINAL_RESIZE': {
            // terminal 应该自动会 resize
            break;
        }
        case 'TERMINAL_SESSION': {
            const paramsStore = useParamsStore();

            const { setting } = storeToRefs(paramsStore);

            terminalId.value = msg.id;
            const sessionInfo = JSON.parse(msg.data);
            const sessionDetail = sessionInfo.session;

            // const username = `${currentUser.value.name} - ${currentUser.value.username}`;
            const username = `${sessionDetail.user}`;

            if (setting.value.SECURITY_WATERMARK_ENABLED) {
                waterMarkContent.value = `${username}\n${sessionDetail.asset.split('(')[0]}`;
            }

            break;
        }
        case 'TERMINAL_SESSION_PAUSE': {
            const data = JSON.parse(msg.data);

            message.info(`${data.user}: ${t('PauseSession')}`);

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
    style: 'width: 35%; padding-bottom: 45px',
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

<style scoped lang="scss">
.common-terminal {
    :deep(.terminal-container) {
        overflow: hidden;

        .xterm-viewport {
            overflow: hidden;
        }

        .xterm-screen {
            height: calc(100vh - 20px) !important;
        }
    }
}
</style>
