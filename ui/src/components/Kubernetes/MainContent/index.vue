<template>
    <n-layout :native-scrollbar="false" content-style="height: 100%">
        <n-tabs
            closable
            ref="el"
            size="small"
            type="card"
            tab="show:lazy"
            tab-style="min-width: 80px;"
            class="header-tab relative"
            v-model:value="nameRef"
            @close="handleClose"
            @update:value="handleChangeTab"
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
                        class="k8s-terminal"
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
                <!-- <TabSuffix /> -->
            </template>
        </n-tabs>
        <!-- <Tip v-if="panels.length === 0" /> -->
    </n-layout>
    <Settings :settings="settings" />
</template>

<script setup lang="ts">
import { storeToRefs } from 'pinia';
import { updateIcon } from '@/components/CustomTerminal/helper';
import { computed, h, markRaw, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import {
    Stop,
    Home,
    Paste,
    Insert,
    NotSent,
    Activity,
    Keyboard,
    UserAvatar,
    ColorPalette,
    Share as ShareIcon
} from '@vicons/carbon';
import { ArrowDown, ArrowUp, ArrowForward, ArrowBack } from '@vicons/ionicons5';

import xtermTheme from 'xterm-theme';
import mittBus from '@/utils/mittBus.ts';

import { useDraggable, type UseDraggableReturn } from 'vue-draggable-plus';

// import Tip from './components/Tip/index.vue';
import Share from '@/components/Share/index.vue';
import Settings from '@/components/Settings/index.vue';
import ThemeConfig from '@/components/ThemeConfig/index.vue';
import CustomTerminal from '@/components/CustomTerminal/index.vue';
// import TabSuffix from '@/components/Kubernetes/MainContent/components/TabSuffix/index.vue';

import { NMessageProvider, TabPaneProps, useDialog, useNotification } from 'naive-ui';

import type { Ref } from 'vue';
import type { ISettingProp, shareUser } from '@/views/interface';
import type { customTreeOption } from '@/hooks/interface';

import { v4 as uuid } from 'uuid';
import { Terminal } from '@xterm/xterm';
import { useI18n } from 'vue-i18n';
import { useMessage } from 'naive-ui';
import { readText } from 'clipboard-polyfill';
import { useLogger } from '@/hooks/useLogger.ts';
import { useTreeStore } from '@/store/modules/tree.ts';
import { useParamsStore } from '@/store/modules/params.ts';
import { useTerminalStore } from '@/store/modules/terminal.ts';

const message = useMessage();
const { debug } = useLogger('K8s-CustomTerminal');

const props = defineProps<{
    socket: WebSocket | undefined;
}>();

const treeStore = useTreeStore();

const { t } = useI18n();
const dialog = useDialog();
const notification = useNotification();

const paramsStore = useParamsStore();
const terminalStore = useTerminalStore();

const { setting } = storeToRefs(paramsStore);
const { connectInfo, treeNodes } = storeToRefs(treeStore);

const el = ref();

const nameRef = ref('');
const sessionId = ref('');
const enableShare = ref(false);
const terminalType = ref('k8s');
const themeName = ref('Default');
const terminalRef: Ref<any[]> = ref([]);
const panels: Ref<TabPaneProps[]> = ref([]);
const userOptions = ref<shareUser[]>([]);

const onlineUsersMap = reactive<{ [key: string]: any }>({});

const settings = computed((): ISettingProp[] => {
    return [
        {
            label: 'ThemeConfig',
            title: t('ThemeConfig'),
            icon: ColorPalette,
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
            label: 'Share',
            title: t('Share'),
            icon: ShareIcon,
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
            label: 'User',
            title: t('User'),
            icon: UserAvatar,
            disabled: () => Object.keys(onlineUsersMap).length < 1,
            content: Object.values(onlineUsersMap)
                .map((item: any) => {
                    item.name = item.user;
                    item.icon = item.writable ? markRaw(Activity) : markRaw(NotSent);
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
        },
        {
            label: 'Keyboard',
            title: t('Keyboard shortcuts'),
            icon: Keyboard,
            content: [
                {
                    name: 'Ctrl + C',
                    icon: Stop,
                    tip: t('Stop'),
                    click: () => {
                        handleWriteData('Stop');
                    }
                },
                {
                    name: 'Command/Ctrl + V',
                    icon: Paste,
                    tip: t('Paste'),
                    click: () => {
                        handleWriteData('Paste');
                    }
                },
                {
                    name: 'Home',
                    icon: Home,
                    tip: t('Home'),
                    click: () => {
                        handleWriteData('Home');
                    }
                },
                {
                    name: 'Insert',
                    icon: Insert,
                    tip: t('Insert'),
                    click: () => {
                        handleWriteData('Insert');
                    }
                },
                {
                    name: 'Arrow Up',
                    icon: ArrowUp,
                    tip: t('ArrowUp'),
                    click: () => {
                        handleWriteData('ArrowUp');
                    }
                },
                {
                    name: 'Arrow Down',
                    icon: ArrowDown,
                    tip: t('ArrowDown'),
                    click: () => {
                        handleWriteData('ArrowDown');
                    }
                },
                {
                    name: 'Arrow Left',
                    icon: ArrowBack,
                    tip: t('ArrowBack'),
                    click: () => {
                        handleWriteData('ArrowLeft');
                    }
                },
                {
                    name: 'Arrow Right',
                    icon: ArrowForward,
                    tip: t('ArrowForward'),
                    click: () => {
                        handleWriteData('ArrowRight');
                    }
                }
            ],
            disabled: () => false,
            click: () => {}
        }
    ];
});

const resetShareDialog = () => {
    paramsStore.setShareId('');
    paramsStore.setShareCode('');
    dialog.destroyAll();
};

const draggable = useDraggable<UseDraggableReturn>(el, panels.value, {
    animation: 150
});

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
        case 'TERMINAL_GET_SHARE_USER': {
            userOptions.value = JSON.parse(msg.data);
            break;
        }
        case 'TERMINAL_SHARE': {
            const data = JSON.parse(msg.data);

            paramsStore.setShareId(data.share_id);
            paramsStore.setShareCode(data.code);

            break;
        }
        case 'CLOSE': {
            enableShare.value = false;

            notification.error({
                content: t('WebSocketClosed'),
                duration: 50000
            });
            break;
        }
        default:
            break;
    }
};

/**
 * 处理标签关闭
 *
 * @param name
 */
const handleClose = (name: string) => {
    const index = panels.value.findIndex(panel => panel.name === name);

    panels.value.splice(index, 1);

    nextTick(() => {
        const panelLength = panels.value.length;

        if (panelLength >= 1) {
            nameRef.value = panels.value[panelLength - 1].name as string;
        }
    });
};

/**
 * 递归查询切换标签时当前 tab 的 key，并重新设置 currentNode
 *
 * @param id
 */
const findNodeById = (id: string): void => {
    const searchNode = (nodes: customTreeOption[]) => {
        for (const node of nodes) {
            if (node.key === id) {
                treeStore.setCurrentNode(node);
                return true;
            }
            if (node.children && node.children.length > 0) {
                const found = searchNode(node.children);
                if (found) return true;
            }
        }
        return false;
    };

    searchNode(treeNodes.value);
};

/**
 * 切换标签
 *
 * @param value
 */
const handleChangeTab = (value: string) => {
    nameRef.value = value;

    findNodeById(value);

    terminalStore.setTerminalConfig('currentTab', nameRef.value);
};

/**
 * 向终端写入快捷命令
 *
 * @param type
 */
const handleWriteData = async (type: string) => {
    if (!terminalRef.value || terminalRef.value.length === 0) {
        message.error(t('No terminal instances available'));
        return;
    }
    const terminalInstance: Terminal = terminalRef.value[0]?.terminalRef;
    if (!terminalInstance) {
        console.error('Terminal instance is not available');
        return;
    }

    switch (type) {
        case 'Paste': {
            terminalInstance.paste(await readText());
            break;
        }
        case 'Stop': {
            terminalInstance.paste('^C');
            break;
        }
        case 'ArrowUp': {
            terminalInstance.paste('\x1b[A');
            break;
        }
        case 'ArrowDown': {
            terminalInstance.paste('\x1b[B');
            break;
        }
        case 'ArrowLeft': {
            terminalInstance.paste('\x1b[D');
            break;
        }
        case 'ArrowRight': {
            terminalInstance.paste('\x1b[C');
            break;
        }
    }

    requestAnimationFrame(() => {
        terminalInstance.focus();
    });
};

onMounted(() => {
    const tabsElement = el.value?.$el?.querySelector('.n-tabs-tab');

    if (tabsElement) {
        // 使用 useDraggable 使 n-tabs 支持拖拽排序
        draggable(tabsElement, panels.value, {
            animation: 150,
            onEnd: event => {
                // 处理拖拽结束后的面板顺序更新
                const movedPanel = panels.value.splice(event.oldIndex, 1)[0];
                panels.value.splice(event.newIndex, 0, movedPanel);

                // 更新当前选中的标签
                if (panels.value.length > 0) {
                    nameRef.value = panels.value[Math.min(event.newIndex, panels.value.length - 1)]
                        .name as string;
                    terminalStore.setTerminalConfig('currentTab', nameRef.value);
                }
            }
        });
    }

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

        treeStore.setCurrentNode(currentNode);

        const sendTerminalData = () => {
            if (terminalRef.value) {
                setTimeout(() => {
                    const terminalInstance = terminalRef.value[0]?.terminalRef;

                    const cols = terminalInstance?.cols;
                    const rows = terminalInstance?.rows;

                    if (cols && rows) {
                        const sendData = {
                            id: currentNode.id,
                            k8s_id: currentNode.k8s_id || uuid(),
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
