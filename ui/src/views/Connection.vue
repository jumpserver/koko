<template>
    <TerminalComponent
        ref="terminalRef"
        :enable-zmodem="true"
        :share-code="shareCode"
        :theme-name="themeName"
        @event="onEvent"
        @ws-data="onWsData"
    />

    <Settings :settings="settings" />
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n';
import { useLogger } from '@/hooks/useLogger';
import { useDialog, useMessage } from 'naive-ui';
import { useTerminal } from '@/hooks/useTerminal.ts';
import { useParamsStore } from '@/store/modules/params.ts';

import { storeToRefs } from 'pinia';
import { Terminal } from '@xterm/xterm';
import { ISettingProp, shareUser } from '@/views/interface';
import { computed, h, nextTick, reactive, Ref, ref } from 'vue';
import { ApertureOutline, PersonOutline, ShareSocialOutline } from '@vicons/ionicons5';

import mittBus from '@/utils/mittBus.ts';
import Share from '@/components/Share/index.vue';
import Settings from '@/components/Settings/index.vue';
import ThemeConfig from '@/components/ThemeConfig/index.vue';
import TerminalComponent from '@/components/Terminal/Terminal.vue';

const paramsStore = useParamsStore();

const { t } = useI18n();
const { setTerminalTheme } = useTerminal();
const { shareCode, setting } = storeToRefs(paramsStore);
const { debug } = useLogger('Connection');

const dialog = useDialog();
const message = useMessage();

const shareId = ref('');
const sessionId = ref('');
const themeName = ref('Default');

const terminalRef = ref(null);

const loading = ref(false);
const userLoading = ref(false);
const enableShare = ref(false);

const onlineUsersMap = reactive<{ [key: string]: any }>({});
const userOptions: Ref<shareUser[]> = ref([]);

const shareTitle = computed((): string => {
    return shareId.value ? t('Share') : t('CreateLink');
});
const settings = computed((): ISettingProp[] => {
    return [
        {
            title: t('ThemeConfig'),
            icon: ApertureOutline,
            disabled: () => false,
            click: () => {
                dialog.success({
                    class: 'set-theme',
                    title: t('Theme'),
                    showIcon: false,
                    style: 'width: 50%',
                    content: () =>
                        h(ThemeConfig, {
                            currentThemeName: themeName.value,
                            preview: (tempTheme: string) => {
                                themeName.value = tempTheme;
                            }
                        })
                });
                // 关闭抽屉
                mittBus.emit('open-setting');
            }
        },
        {
            title: t('Share'),
            icon: ShareSocialOutline,
            disabled: () => !enableShare.value,
            click: () => {
                dialog.success({
                    class: 'share',
                    title: shareTitle.value,
                    showIcon: false,
                    style: 'width: 35%',
                    content: () =>
                        h(Share, {
                            shareId: shareId.value,
                            shareCode: shareCode.value,
                            sessionId: sessionId.value,
                            enableShare: enableShare.value,
                            userOptions: userOptions.value
                        })
                });
                // 关闭抽屉
                mittBus.emit('open-setting');
            }
        },
        {
            title: t('User'),
            icon: PersonOutline,
            disabled: () => Object.keys(onlineUsersMap).length < 1,
            content: Object.values(onlineUsersMap)
                .map((item: any) => {
                    item.name = item.user;
                    item.faIcon = item.writable ? 'fa-solid fa-keyboard' : 'fa-solid fa-eye';
                    item.iconTip = item.writable ? t('Writable') : t('ReadOnly');
                    return item;
                })
                .sort((a, b) => new Date(a.created).getTime() - new Date(b.created).getTime()),
            click: () => {}
        }
    ];
});

const onWsData = (msgType: string, msg: any, terminal: Terminal) => {
    switch (msgType) {
        case 'TERMINAL_SESSION': {
            const sessionInfo = JSON.parse(msg.data);
            const sessionDetail = sessionInfo.session;

            debug(`SessionDetail themeName: ${sessionInfo.themeName}`);
            debug(`SessionDetail permissions: ${sessionInfo.permission}`);
            debug(`SessionDetail ctrlCAsCtrlZ: ${sessionInfo.ctrlCAsCtrlZ}`);
            debug(`SessionDetail backspaceAsCtrlH: ${sessionInfo.backspaceAsCtrlH}`);

            const share = sessionInfo.permission.actions.includes('share');

            if (sessionInfo.backspaceAsCtrlH) {
                const value = sessionInfo.backspaceAsCtrlH ? '1' : '0';
                debug(`Set backspaceAsCtrlH: ${value}`);

                // terminal.options.backspaceAsCtrlH = value;
            }

            if (sessionInfo.ctrlCAsCtrlZ) {
                const value = sessionInfo.ctrlCAsCtrlZ ? '1' : '0';
                debug(`Set ctrlCAsCtrlZ: ${value}`);

                // terminal.options.ctrlCAsCtrlZ = value;
            }

            if (setting.value.SECURITY_SESSION_SHARE && share) {
                enableShare.value = true;
            }

            sessionId.value = sessionDetail.id;
            themeName.value = sessionInfo.themeName;

            nextTick(() => {
                setTerminalTheme(themeName.value, terminal);
            });

            break;
        }
        case 'TERMINAL_SHARE': {
            const data = JSON.parse(msg.data);

            shareId.value = data.share_id;
            paramsStore.setShareCode(data.code);

            loading.value = false;
            break;
        }
        case 'TERMINAL_SHARE_JOIN': {
            const data = JSON.parse(msg.data);

            const key: string = data.terminal_id;

            onlineUsersMap[key] = data;

            // debug('onlineUsersMap', onlineUsersMap);

            if (data.primary) {
                debug('Primary User 不提醒');
                break;
            }

            message.info(`${data.user} ${t('JoinShare')}`);
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
        case 'TERMINAL_GET_SHARE_USER': {
            userLoading.value = false;
            userOptions.value = JSON.parse(msg.data);
            break;
        }
        case 'TERMINAL_SESSION_PAUSE': {
            const data = JSON.parse(msg.data);

            message.info(`${data.user} ${t('PauseSession')}`);
            break;
        }
        case 'TERMINAL_SESSION_RESUME': {
            const data = JSON.parse(msg.data);

            message.info(`${data.user} ${t('ResumeSession')}`);
            break;
        }
        default:
            break;
    }

    debug('On WebSocket Data:', msg);
};
const onEvent = (event: string, data: any) => {
    switch (event) {
        case 'reconnect':
            debug('Reconnect');
            // Object.keys(onlineUsersMap.value).filter(key => {
            //
            // });
            console.log(data);
            break;
        case 'open':
            debug('Open');
            mittBus.emit('open-setting');
            break;
    }
};
</script>
