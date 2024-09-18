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
            @contextmenu.prevent="handleContextMenu"
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
                    <n-watermark
                        cross
                        selectable
                        :rotate="-45"
                        :font-size="20"
                        :width="300"
                        :height="300"
                        :content="waterMarkContent"
                        :line-height="20"
                        :x-offset="-60"
                        :y-offset="60"
                        :font-family="'Open Sans'"
                    >
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
                    </n-watermark>
                </n-scrollbar>
            </n-tab-pane>
        </n-tabs>
    </n-layout>
    <n-dropdown
        show-arrow
        size="medium"
        trigger="manual"
        placement="bottom-start"
        content-style='font-size: "13px"'
        :x="dropdownX"
        :y="dropdownY"
        :show="showContextMenu"
        :options="contextMenuOption"
        @select="handleContextMenuSelect"
        @clickoutside="handleClickOutside"
    />
    <Settings :settings="settings" />
</template>

<script setup lang="ts">
import { storeToRefs } from 'pinia';
import { updateIcon } from '@/components/CustomTerminal/helper';
import { Component, Ref } from 'vue';
import { computed, h, markRaw, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import {
    Activity,
    ColorPalette,
    Keyboard,
    NotSent,
    Paste,
    Share as ShareIcon,
    Stop,
    UserAvatar
} from '@vicons/carbon';
// @ts-ignore
import { CloneRegular } from '@vicons/fa';
import { RefreshFilled } from '@vicons/material';
import { ClosedCaption32Regular } from '@vicons/fluent';
import { ArrowBack, ArrowDown, ArrowForward, ArrowUp, CloseCircleOutline } from '@vicons/ionicons5';

import xtermTheme from 'xterm-theme';
import mittBus from '@/utils/mittBus.ts';

import { useDraggable, type UseDraggableReturn } from 'vue-draggable-plus';

import Share from '@/components/Share/index.vue';
import Settings from '@/components/Settings/index.vue';
import ThemeConfig from '@/components/ThemeConfig/index.vue';
import CustomTerminal from '@/components/CustomTerminal/index.vue';

import { DropdownOption, NIcon, NMessageProvider, TabPaneProps, useDialog, useMessage } from 'naive-ui';
import type { ISettingProp, shareUser } from '@/views/interface';

import { v4 as uuid } from 'uuid';
import { Terminal } from '@xterm/xterm';
import { useI18n } from 'vue-i18n';
import { readText } from 'clipboard-polyfill';
import { useLogger } from '@/hooks/useLogger.ts';
import { useTreeStore } from '@/store/modules/tree.ts';
import { useParamsStore } from '@/store/modules/params.ts';
import { useTerminalStore } from '@/store/modules/terminal.ts';
import { useDebounceFn } from '@vueuse/core';

const message = useMessage();
const { debug } = useLogger('K8s-CustomTerminal');

const props = defineProps<{
    socket: WebSocket | undefined;
}>();

const { t } = useI18n();
const dialog = useDialog();

const treeStore = useTreeStore();
const paramsStore = useParamsStore();
const terminalStore = useTerminalStore();

const { setting } = storeToRefs(paramsStore);
const { currentTab } = storeToRefs(terminalStore);
const { connectInfo, currentNode, terminalMap } = storeToRefs(treeStore);

const el = ref();

const dropdownY = ref(0);
const dropdownX = ref(0);
const deleteUserCounter = ref(0);
const nameRef = ref('');
const sessionId = ref('');
const waterMarkContent = ref('');
const enableShare = ref(false);
const showContextMenu = ref(false);
const terminalType = ref('k8s');
const themeName = ref('Default');
const contextIdentification = ref('');
const terminalRef: Ref<any[]> = ref([]);
const panels: Ref<TabPaneProps[]> = ref([]);
const userOptions = ref<shareUser[]>([]);

const processedElements = new Set();
const sessionIdMap = new Map();

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
                    style: 'width: 50%; min-width: 810px',
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
                const sessionId = sessionIdMap.get(currentTab.value);

                dialog.success({
                    class: 'share',
                    title: t('CreateLink'),
                    showIcon: false,
                    style: 'width: 35%; min-width: 500px',
                    content: () => {
                        return h(NMessageProvider, null, {
                            default: () =>
                                h(Share, {
                                    sessionId,
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
                        const sessionId = sessionIdMap.get(currentTab.value);

                        mittBus.emit('remove-share-user', {
                            sessionId: sessionId,
                            userMeta: user,
                            type: 'TERMINAL_SHARE_USER_REMOVE'
                        });
                    }
                });
            }
        },
        {
            label: 'Keyboard',
            title: t('Hotkeys'),
            icon: Keyboard,
            content: [
                {
                    name: 'Ctrl + C',
                    icon: Stop,
                    tip: t('Cancel'),
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
                    name: 'Arrow Up',
                    icon: ArrowUp,
                    tip: t('UpArrow'),
                    click: () => {
                        handleWriteData('ArrowUp');
                    }
                },
                {
                    name: 'Arrow Down',
                    icon: ArrowDown,
                    tip: t('DownArrow'),
                    click: () => {
                        handleWriteData('ArrowDown');
                    }
                },
                {
                    name: 'Arrow Left',
                    icon: ArrowBack,
                    tip: t('LeftArrow'),
                    click: () => {
                        handleWriteData('ArrowLeft');
                    }
                },
                {
                    name: 'Arrow Right',
                    icon: ArrowForward,
                    tip: t('RightArrow'),
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

const contextMenuOption = reactive([
    {
        label: t('Reconnect'),
        key: 'reconnect',
        icon: renderIcon(RefreshFilled)
    },
    {
        label: t('Close Current Tab'),
        key: 'close',
        icon: renderIcon(CloseCircleOutline)
    },
    {
        label: t('Close All Tabs'),
        key: 'closeAll',
        icon: renderIcon(ClosedCaption32Regular)
    }
    // {
    //     label: t('Clone Connect'),
    //     key: 'cloneConnect',
    //     icon: renderIcon(CloneRegular)
    // }
]);

/**
 * 用 h 函数渲染图标
 */
function renderIcon(icon: Component) {
    return () => {
        return h(NIcon, null, {
            default: () => h(icon)
        });
    };
}

/**
 * 重连事件的回调
 */
const handleReconnect = () => {
    try {
        findNodeById(contextIdentification.value);

        delete currentNode.value.skip;

        const terminal: Terminal = currentNode.value?.terminal as Terminal;

        if (terminal) {
            terminal.reset();

            nextTick(() => {
                mittBus.emit('connect-terminal', { skip: true, ...currentNode.value });

                showContextMenu.value = false;
            });
        }
    } catch (e) {}
};

/**
 * 右键菜单的回调
 *
 * @param key
 * @param _option
 */
const handleContextMenuSelect = (key: string, _option: DropdownOption) => {
    switch (key) {
        case 'reconnect': {
            handleReconnect();
            break;
        }
        case 'close': {
            contextIdentification.value ? handleClose(contextIdentification.value) : '';

            showContextMenu.value = false;
            break;
        }
        case 'closeAll': {
            panels.value = [];
            showContextMenu.value = false;
            break;
        }
        case 'cloneConnect': {
            // findNodeById(contextIdentification.value);
            delete currentNode.value.skip;

            nextTick(() => {
                mittBus.emit('connect-terminal', { skip: false, ...currentNode.value });
                showContextMenu.value = false;

                console.log(panels);
                console.log(currentNode.value);
            });

            break;
        }
    }
};

/**
 * 每个 tab 标签的右侧快捷功能
 */
const handleContextMenu = (e: PointerEvent) => {
    let target: HTMLElement = e.target as HTMLElement;

    while (target && !target.hasAttribute('data-name')) {
        target = target.parentElement as HTMLElement;
    }

    if (target) {
        // 获取设置的 data 属性
        const dataName: string = target.getAttribute('data-name') as string;

        if (dataName) {
            contextIdentification.value = dataName;

            e.preventDefault();
            showContextMenu.value = true;
            dropdownY.value = e.clientY;
            dropdownX.value = e.clientX;
        }
    }
};

/**
 * 关闭右侧菜单
 */
const handleClickOutside = () => {
    showContextMenu.value = false;
};

/**
 * 重置分享表单的数据
 */
const resetShareDialog = () => {
    paramsStore.setShareId('');
    paramsStore.setShareCode('');
    dialog.destroyAll();
};

/**
 * 交换数组中的某两个值
 *
 * @param arr
 * @param index1
 * @param index2
 */
const swapElements = (arr: any[], index1: number, index2: number) => {
    [arr[index1], arr[index2]] = [arr[index2], arr[index1]];
    return arr;
};

/**
 * 拖拽事件
 */
const initializeDraggable = () => {
    const tabsContainer = document.querySelector('.n-tabs-wrapper');

    if (tabsContainer) {
        // 对于 useDraggable 如果直接操作 panel 可能会导致被注入一个 undefined 值从而导致报错，因此下面代码全部使用副本来操作
        // @ts-ignore
        useDraggable<UseDraggableReturn>(tabsContainer, JSON.parse(JSON.stringify(panels.value)), {
            animation: 150,
            onEnd: async event => {
                if (!event || event.newIndex === undefined || event.oldIndex === undefined) {
                    return console.warn('Event or index is undefined');
                }

                let newIndex = event!.newIndex - 1;
                let oldIndex = event!.oldIndex - 1;

                const clonedPanels = JSON.parse(JSON.stringify(panels.value));

                panels.value = swapElements(clonedPanels, newIndex, oldIndex).filter(panel => panel !== null);

                const newActiveTab: string = panels.value[newIndex!]?.name as string;

                if (newActiveTab) {
                    nameRef.value = newActiveTab;
                    findNodeById(newActiveTab);
                    terminalStore.setTerminalConfig('currentTab', newActiveTab);
                }

                await nextTick(() => {});
            }
        });
    }
};

/**
 * 抛出外层的 socket 事件
 *
 * @param msgType
 * @param msg
 * @param terminal
 */
const onSocketData = (msgType: string, msg: any, terminal: Terminal) => {
    switch (msgType) {
        case 'TERMINAL_SESSION':
            const sessionInfo = JSON.parse(msg.data);
            const sessionDetail = sessionInfo.session;

            const share = sessionInfo.permission.actions.includes('share');
            const username = `${sessionDetail.user}`;

            if (setting.value.SECURITY_WATERMARK_ENABLED) {
                waterMarkContent.value = `${username}\n${sessionDetail.asset.split('(')[0]}`;
            }

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

            const currentK8sId: string = currentNode.value?.k8s_id as string;

            if (currentK8sId) {
                sessionIdMap.set(currentK8sId, sessionDetail.id);
            }

            // sessionId.value = sessionDetail.id;
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
            break;
        }
        case 'K8S_CLOSE': {
            enableShare.value = false;

            deleteUserCounter.value--;

            // 用于删除根用户
            if (deleteUserCounter.value === 0) {
                for (const key in onlineUsersMap) {
                    delete onlineUsersMap[key];
                }
            }

            // 用于删除分享的用户
            if (onlineUsersMap.hasOwnProperty(msg.id)) {
                delete onlineUsersMap[msg.id];
            }

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
    const index = panels.value.findIndex(panel => {
        // @ts-ignore
        const node = treeStore.getTerminalByK8sId(panel.name);
        const socket = node?.socket;

        if (socket) {
            socket.send(
                JSON.stringify({
                    type: 'K8S_CLOSE',
                    id: node.id,
                    k8s_id: node.k8s_id
                })
            );
        }

        return panel.name === name;
    });

    panels.value.splice(index, 1);

    const panelLength = panels.value.length;

    if (panelLength >= 1) {
        nameRef.value = panels.value[panelLength - 1].name as string;
        findNodeById(nameRef.value);
        terminalStore.setTerminalConfig('currentTab', nameRef.value);
    }
};

/**
 * 递归查询切换标签时当前 tab 的 key，并重新设置 currentNode
 *
 * @param nameRef
 */
const findNodeById = (nameRef: string): void => {
    for (const [_key, value] of terminalMap.value.entries()) {
        if (value.k8s_id === nameRef) {
            treeStore.setCurrentNode(value);
        }
    }
};

const updateTabElements = (key: string) => {
    const tabElements = document.querySelectorAll('.n-tabs-tab-wrapper');

    tabElements.forEach(element => {
        if (!processedElements.has(element)) {
            element.setAttribute('data-identification', key);
            processedElements.add(element);
        }
    });
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

/**
 * 切换到上一个 Tab
 */
const switchToPreviousTab = () => {
    const currentIndex = panels.value.findIndex(panel => panel.name === nameRef.value);

    if (currentIndex > 0) {
        nameRef.value = panels.value[currentIndex - 1].name as string;
    } else {
        nameRef.value = panels.value[panels.value.length - 1].name as string;
    }

    findNodeById(nameRef.value);

    terminalStore.setTerminalConfig('currentTab', nameRef.value);
};

/**
 * 切换到下一个 Tab
 */
const switchToNextTab = () => {
    const currentIndex = panels.value.findIndex(panel => panel.name === nameRef.value);

    if (currentIndex < panels.value.length - 1) {
        nameRef.value = panels.value[currentIndex + 1].name as string;
    } else {
        nameRef.value = panels.value[0].name as string;
    }

    findNodeById(nameRef.value);

    terminalStore.setTerminalConfig('currentTab', nameRef.value);
};

onMounted(() => {
    nextTick(() => {
        initializeDraggable();
    });

    mittBus.on('connect-terminal', currentNode => {
        let existingPanelName = uuid();

        const has = treeStore.getTerminalByK8sId(currentNode.k8s_id as string);

        if (!has) {
            panels.value.push({
                name: currentNode.key,
                tab: currentNode.label
            });
        }

        if (has && !currentNode.skip) {
            panels.value.push({
                name: existingPanelName as string,
                tab: currentNode.label
            });

            currentNode.key = existingPanelName;
            currentNode.k8s_id = existingPanelName;
        }

        // todo 冗余代码，后期优化
        if (currentNode.skip) {
            // 用于 contentMenu 找到当前的唯一标识
            nextTick(() => {
                updateTabElements(currentNode?.key as string);
            });

            console.log(currentNode);
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
                                k8s_id: currentNode.k8s_id,
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
            deleteUserCounter.value++;

            return;
        }

        // 用于 contentMenu 找到当前的唯一标识
        nextTick(() => {
            updateTabElements(currentNode?.key as string);
        });

        treeStore.setCurrentNode(currentNode);

        const sendTerminalData = () => {
            if (terminalRef.value) {
                setTimeout(() => {
                    // todo 优化
                    const terminalInstance = terminalRef.value[0]?.terminalRef;

                    const cols = terminalInstance?.cols;
                    const rows = terminalInstance?.rows;

                    if (cols && rows) {
                        const sendData = {
                            id: currentNode.id,
                            k8s_id: currentNode.k8s_id,
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
        deleteUserCounter.value++;
    });

    const debouncedSwitchToPreviousTab = useDebounceFn(() => {
        switchToPreviousTab();
    }, 200);

    const debouncedSwitchToNextTab = useDebounceFn(() => {
        switchToNextTab();
    }, 200);

    mittBus.on('alt-shift-left', debouncedSwitchToPreviousTab);
    mittBus.on('alt-shift-right', debouncedSwitchToNextTab);
});

onBeforeUnmount(() => {
    mittBus.off('alt-shift-left', switchToPreviousTab);
    mittBus.off('alt-shift-right', switchToNextTab);
    mittBus.off('connect-terminal');
});
</script>

<style scoped lang="scss">
@import './index.scss';
</style>
