<template>
    <div id="terminal"></div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import TerminalManager from './helper/TerminalManager';
import WebSocketManager from './helper/WebSocketManager';
import type { ITerminalProps } from '../interface';
import { Terminal } from '@xterm/xterm';
import { useLogger } from '@/hooks/useLogger.ts';
import ZmodemBrowser from 'nora-zmodemjs/src/zmodem_browser';
import { bytesHuman } from '@/utils';
import { useI18n } from 'vue-i18n';
import { useMessage } from 'naive-ui';

const { t } = useI18n();
const message = useMessage();
const { info } = useLogger();

const props = withDefaults(defineProps<ITerminalProps>(), {
    themeName: 'Default',
    enableZmodem: false
});

const MAX_TRANSFER_SIZE = 1024 * 1024 * 500; // 默认最大上传下载500M

const zsentry = ref<any>();
const lastSendTime = ref(new Date());
const zmodeSession = ref(null);
const zmodeDialogVisible = ref(false);
const fileList = ref([]);

const terminalManager = new TerminalManager();
const webSocketManager = new WebSocketManager(lastSendTime.value as Date, props.enableZmodem);

const term = terminalManager.term;

const handleSendSession = (zsession: any) => {
    zmodeSession.value = zsession;
    zmodeDialogVisible.value = true;

    zsession.on('session_end', () => {
        zmodeSession.value = null;
        fileList.value = [];
        term?.write('\r\n');
        // this.$refs.upload.clearFiles();
    });
};

const handleReceiveSession = (zsession: any) => {
    zsession.on('offer', (xfer: any) => {
        const buffer = [];
        const detail = xfer.get_details();
        if (detail.size >= MAX_TRANSFER_SIZE) {
            const msg = t('ExceedTransferSize') + ': ' + bytesHuman(MAX_TRANSFER_SIZE);
            info(msg);
            message.info(msg);
            xfer.skip();
            return;
        }
        xfer.on('input', (payload: any) => {
            // updateReceiveProgress(xfer);
            buffer.push(new Uint8Array(payload));
        });
        xfer.accept().then(() => {
            // this.saveToDisk(xfer, buffer);
            message.info(t('DownloadSuccess') + ' ' + detail.name);
            term?.write('\r\n');
            zmodeSession.value?.abort();
        }, console.error.bind(console));
    });
};

const connect = async () => {
    info(`connectURL: ${props.connectURL}`);

    const el = document.getElementById('terminal');
    await terminalManager.createTerminal(el as HTMLElement);

    const term = terminalManager.term;

    info(`ZmodemBrowser: ${ZmodemBrowser}`);

    zsentry.value = new ZmodemBrowser.Sentry({
        to_terminal: (octets: any) => {
            if (zsentry.value && !zsentry.value.get_confirmed_session()) {
                term?.write(octets);
            }
        },
        sender: (octets: any) => {
            if (!webSocketManager.isWsActivated()) {
                return info('websocket closed');
            }
            lastSendTime.value = new Date();
            webSocketManager.ws?.send(new Uint8Array(octets));
        },
        on_retract: () => {
            info('zmodem Retract');
        },
        on_detect: (detection: any) => {
            const zsession = detection.confirm();
            term?.write('\r\n');

            if (zsession.type === 'send') {
                handleSendSession(zsession);
            } else {
                handleReceiveSession(zsession);
            }
        }
    });

    // 创建 WebSocket 连接
    console.log(term);
    webSocketManager.connectWs(props.connectURL, term as Terminal, zsentry);

    // term?.onData(data => {});
    // term?.onResize(({ cols, rows }) => {});
};

const registerJMSEvent = () => {};

onMounted(() => {
    registerJMSEvent();
    connect();
});
</script>

<style scoped></style>
