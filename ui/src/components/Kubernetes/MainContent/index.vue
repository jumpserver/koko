<template>
    <n-layout :native-scrollbar="false" content-style="height: 100%">
        <n-tabs
            closable
            size="small"
            type="card"
            tab="show:lazy"
            tab-style="min-width: 80px;"
            v-model:value="nameRef"
            @close="handleClose"
            @update:value="handleChangeTab"
            class="header-tab relative"
        >
            <n-tab-pane
                v-for="panel of panels"
                :key="panel.name"
                :tab="panel.tab"
                :name="panel.name"
                display-directive="show:lazy"
                class="bg-[#101014] pt-0"
            >
                <n-scrollbar trigger="hover">
                    <CustomTerminal
                        ref="terminalRef"
                        :socket="socket"
                        :key="panel.name"
                        :index-key="panel.name as string"
                        :theme-name="themeName"
                        :terminal-type="terminalType"
                        @socketData="onSocketData"
                    />
                </n-scrollbar>
            </n-tab-pane>
            <template v-slot:suffix>
                <n-flex
                    justify="space-between"
                    align="center"
                    class="h-[35px] mr-[15px]"
                    style="column-gap: 5px"
                >
                    <n-popover>
                        <template #trigger>
                            <div
                                class="icon-item flex justify-center items-center w-[25px] h-[25px] cursor-pointer transition-all duration-300 ease-in-out hover:rounded-[5px]"
                            >
                                <svg-icon name="split" :icon-style="iconStyle" />
                            </div>
                        </template>
                        拆分
                    </n-popover>

                    <n-popover>
                        <template #trigger>
                            <div
                                class="icon-item flex justify-center items-center w-[25px] h-[25px] cursor-pointer transition-all duration-300 ease-in-out hover:rounded-[5px]"
                            >
                                <n-icon size="16px" :component="EllipsisHorizontal" />
                            </div>
                        </template>
                        操作
                    </n-popover>
                </n-flex>
            </template>
        </n-tabs>
        <Main v-if="panels.length === 0" />
    </n-layout>
    <Settings :settings="settings" />
</template>

<script setup lang="ts">
import { storeToRefs } from 'pinia';
import { updateIcon } from '@/components/CustomTerminal/helper';
import { computed, h, markRaw, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import {
    ApertureOutline,
    EllipsisHorizontal,
    LockClosedOutline,
    PersonAdd,
    PersonOutline,
    ShareSocialOutline
} from '@vicons/ionicons5';

import Main from '../Main/index.vue';
import xtermTheme from 'xterm-theme';
import mittBus from '@/utils/mittBus.ts';
import Share from '@/components/Share/index.vue';
import Settings from '@/components/Settings/index.vue';
import ThemeConfig from '@/components/ThemeConfig/index.vue';
import CustomTerminal from '@/components/CustomTerminal/index.vue';

// 引入 type
import { NMessageProvider, TabPaneProps, useDialog } from 'naive-ui';

import type { CSSProperties, Ref } from 'vue';
import type { ISettingProp, shareUser } from '@/views/interface';

// 引入 hook
import { useI18n } from 'vue-i18n';
import { v4 as uuidv4 } from 'uuid';
import { useMessage } from 'naive-ui';
import { Terminal } from '@xterm/xterm';
import { useLogger } from '@/hooks/useLogger.ts';
import { useTreeStore } from '@/store/modules/tree.ts';
import { useParamsStore } from '@/store/modules/params.ts';
import { useTerminalStore } from '@/store/modules/terminal.ts';

// 图标样式
const iconStyle: CSSProperties = {
    width: '16px',
    height: '16px',
    transition: 'fill 0.3s'
};

// 创建消息和日志实例
const message = useMessage();
const { debug } = useLogger('K8s-CustomTerminal');

const props = defineProps<{
    socket: WebSocket | undefined;
}>();

const treeStore = useTreeStore();

const { t } = useI18n();
const dialog = useDialog();
const paramsStore = useParamsStore();
const terminalStore = useTerminalStore();

const { setting } = storeToRefs(paramsStore);
const { connectInfo } = storeToRefs(treeStore);

const nameRef = ref('');
const sessionId = ref('');
const terminalType = ref('k8s');
const themeName = ref('Default');
const enableShare = ref(false);
const terminalRef: Ref<any[]> = ref([]);
const panels: Ref<TabPaneProps[]> = ref([]);
const userOptions = ref<shareUser[]>([]);

const onlineUsersMap = reactive<{ [key: string]: any }>({});

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
                    title: t('CreateLink'),
                    showIcon: false,
                    style: 'width: 35%',
                    content: () => {
                        return h(NMessageProvider, null, {
                            default: () =>
                                h(Share, {
                                    sessionId: sessionId.value,
                                    enableShare: enableShare.value,
                                    userOptions: userOptions.value
                                })
                        });
                    },
                    onClose: () => resetShareDialog(),
                    onMaskClick: () => resetShareDialog()
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
                    item.icon = item.writable ? markRaw(PersonAdd) : markRaw(LockClosedOutline);
                    item.tip = item.writable ? t('Writable') : t('ReadOnly');
                    return item;
                })
                .sort((a, b) => new Date(a.created).getTime() - new Date(b.created).getTime()),
            click: user => {
                if (user.primary) return;

                dialog.warning({
                    title: '警告',
                    content: t('RemoveShareUserConfirm'),
                    positiveText: '确定',
                    negativeText: '取消',
                    onPositiveClick: () => {
                        mittBus.emit('remove-share-user', {
                            sessionId: sessionId.value,
                            userMeta: user,
                            type: 'TERMINAL_SHARE_USER_REMOVE'
                        });
                    }
                });
            }
        }
    ];
});

const resetShareDialog = () => {
    paramsStore.setShareId('');
    paramsStore.setShareCode('');
    dialog.destroyAll();
};

const onSocketData = (msgType: string, msg: any, terminal: Terminal) => {
    switch (msgType) {
        case 'TERMINAL_SESSION':
            const sessionInfo = JSON.parse(msg.data);
            const sessionDetail = sessionInfo.session;

            const share = sessionInfo.permission.actions.includes('share');

            if (sessionInfo.backspaceAsCtrlH) {
                const value = sessionInfo.backspaceAsCtrlH ? '1' : '0';
                debug(`Set backspaceAsCtrlH: ${value}`);

                terminalStore.setTerminalConfig('backspaceAsCtrlH', value);
            }

            if (sessionInfo.ctrlCAsCtrlZ) {
                const value = sessionInfo.ctrlCAsCtrlZ ? '1' : '0';
                debug(`Set ctrlCAsCtrlZ: ${value}`);

                terminalStore.setTerminalConfig('ctrlCAsCtrlZ', value);
            }

            if (setting.value.SECURITY_SESSION_SHARE && share) {
                enableShare.value = true;
            }

            sessionId.value = sessionDetail.id;
            themeName.value = sessionInfo.themeName;

            nextTick(() => {
                terminal.options.theme = xtermTheme[themeName.value];
            });
            break;
        case 'TERMINAL_SHARE_JOIN':
            const data = JSON.parse(msg.data);

            const key: string = data.terminal_id;

            onlineUsersMap[key] = data;

            if (data.primary) {
                debug('Primary User 不提醒');
                break;
            }

            message.info(`${data.user} ${t('JoinShare')}`);
            break;
        case 'TERMINAL_SHARE_LEAVE': {
            const data = JSON.parse(msg.data);
            const key = data.terminal_id;

            if (onlineUsersMap.hasOwnProperty(key)) {
                delete onlineUsersMap[key];
            }

            message.info(`${data.user} ${t('LeaveShare')}`);
            break;
        }
        case 'TERMINAL_SHARE': {
            const data = JSON.parse(msg.data);

            paramsStore.setShareId(data.share_id);
            paramsStore.setShareCode(data.code);

            break;
        }
        default:
            break;
    }
};

// 处理关闭标签页事件
const handleClose = (name: string) => {
    message.info(`已关闭: ${name}`);
    const index = panels.value.findIndex(panel => panel.name === name);
    panels.value.splice(index, 1);
};

const handleChangeTab = (value: string) => {
    nameRef.value = value;

    terminalStore.setTerminalConfig('currentTab', nameRef.value);
};

onMounted(() => {
    mittBus.on('connect-terminal', currentNode => {
        // 检查 currentNode.key 是否已经存在
        const existingPanel = panels.value.find(panel => panel.name === currentNode.key);

        // 如果存在，直接切换到已有的标签页
        if (existingPanel) {
            nameRef.value = existingPanel.name as string;
            return;
        }

        // 如果不存在，则添加新的标签页
        panels.value.push({
            name: currentNode.key,
            tab: currentNode.label
        });

        const sendTerminalData = () => {
            if (terminalRef.value) {
                nextTick(() => {
                    const terminalInstance = terminalRef.value[0]?.terminalRef;
                    const cols = terminalInstance?.cols;
                    const rows = terminalInstance?.rows;

                    if (cols && rows) {
                        const sendData = {
                            id: currentNode.id,
                            k8s_id: currentNode.k8s_id || uuidv4(),
                            namespace: currentNode.namespace || '',
                            pod: currentNode.pod || '',
                            container: currentNode.container || '',
                            type: 'TERMINAL_K8S_INIT',
                            data: JSON.stringify({
                                cols,
                                rows,
                                code: ''
                            })
                        };

                        updateIcon(connectInfo.value.setting);
                        props.socket?.send(JSON.stringify(sendData));
                    } else {
                        console.error('Failed to get terminal dimensions');
                    }
                });
            } else {
                console.error('CustomTerminal ref is not available');
            }
        };

        sendTerminalData();

        const key: string = currentNode.key as string;

        nameRef.value = key;
        terminalStore.setTerminalConfig('currentTab', key);

        debug('currentNode', currentNode);
    });
});

onBeforeUnmount(() => {
    mittBus.off('connect-terminal');
});
</script>

<style scoped lang="scss">
@import './index.scss';
</style>
