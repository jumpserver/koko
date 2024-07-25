import { useI18n } from 'vue-i18n';
import { Terminal } from '@xterm/xterm';
import { useLogger } from '@/hooks/useLogger.ts';
import { fireEvent } from '@/utils';
import { createDiscreteApi } from 'naive-ui';

const { message } = createDiscreteApi(['message']);

class WebSocketManager {
    public ws: WebSocket | null = null;

    private readonly lastSendTime;

    private term: Terminal | null = null;
    private pingInterval: any = null;
    private lastReceiveTime: Date | null = null;
    private zmodemStatus: boolean = false;
    private enableZmodem: boolean = true;
    private terminalId: string = '';
    private zsentry: any = null;

    private MaxTimeout: number = 30 * 1000;
    private zmodemEnd: string = 'ZMODEM_END';
    private zmodemStart: string = 'ZMODEM_START';

    constructor(lastSendTime: Date, enableZmodem: boolean) {
        this.lastSendTime = lastSendTime;
        this.enableZmodem = enableZmodem;
    }

    private handleError(e: Event) {
        const { info } = useLogger();
        info(`Error: ${e}`);
    }

    private message(id: string, type: string, data: any): string {
        return JSON.stringify({
            id,
            type,
            data
        });
    }

    private writeBufferToTerminal(data: any) {
        const { info } = useLogger();
        if (!this.enableZmodem && this.zmodemStatus) {
            info('未开启 ZMODEM 且当前在 ZMODEM 状态，不允许显示');
            return;
        }
        this.term?.write(new Uint8Array(data));
    }

    private dispatch(data: any) {
        // const { t } = useI18n();
        // const { info } = useLogger();
        if (data === undefined) {
            return;
        }
        let msg = JSON.parse(data);
        switch (msg.type) {
            case 'CONNECT': {
                this.terminalId = msg.id;
                try {
                    // this.fitAddon.fit();
                } catch (e) {
                    // info(`Error: ${e}`);
                }
                const data = {
                    cols: this.term?.cols,
                    rows: this.term?.rows
                    // code: this.code
                };
                const messageInfo = JSON.parse(msg.data);
                // this.currentUser = messageInfo.user;
                // this.setting = messageInfo.setting;
                // info(this.currentUser);
                // this.updateIcon();
                this.ws?.send(this.message(this.terminalId, 'TERMINAL_INIT', JSON.stringify(data)));
                break;
            }
            case 'CLOSE':
                this.term?.writeln('Receive Connection closed');
                this.ws?.close();
                // this.sendEventToLuna('CLOSE', '')
                break;
            case 'PING':
                break;
            case 'TERMINAL_ACTION': {
                const action = msg.data;
                switch (action) {
                    case this.zmodemStart:
                        this.zmodemStatus = true;
                        if (!this.enableZmodem) {
                            // 等待用户 rz sz 文件传输
                            // message.info(t('WaitFileTransfer'));
                        }
                        break;
                    case this.zmodemEnd:
                        if (!this.enableZmodem && this.zmodemStatus) {
                            // message.info(t('EndFileTransfer'));
                            this.term?.write('\r\n');
                        }
                        this.zmodemStatus = false;
                        break;
                    default:
                        this.zmodemStatus = false;
                }
                break;
            }
            case 'TERMINAL_ERROR':
            case 'ERROR': {
                const errMsg = msg.err;
                message.error(errMsg);
                this.term?.writeln(errMsg);
                break;
            }
            case 'MESSAGE_NOTIFY': {
                const errMsg = msg.err;
                const eventData = JSON.parse(msg.data);

                const eventName = eventData.event_name;
                switch (eventName) {
                    case 'sync_user_preference':
                        if (errMsg === '' || errMsg === null) {
                            // const successNotify = t('SyncUserPreferenceSuccess');
                            const successNotify = 'SyncUserPreferenceSuccess';
                            message.success(successNotify);
                        } else {
                            // const errNotify = `${t('SyncUserPreferenceFailed')}: ${errMsg}`;
                            const errNotify = `SyncUserPreferenceFailed': ${errMsg}`;
                            message.error(errNotify);
                        }
                        break;
                    default:
                    // info(`unknown: ${eventName}`);
                }
                break;
            }
            default:
            // info(`default: ${data}`);
        }
        // this.$emit('ws-data', msg.type, msg)
    }

    private onWebsocketOpen() {
        // const { info } = useLogger();
        if (this.pingInterval !== null) {
            clearInterval(this.pingInterval);
        }

        this.lastReceiveTime = new Date();

        this.pingInterval = setInterval(() => {
            if (
                this.ws?.readyState === WebSocket.CLOSING ||
                this.ws?.readyState === WebSocket.CLOSED
            ) {
                clearInterval(this.pingInterval);
                return;
            }

            let currentDate: Date = new Date();

            if (
                this.lastReceiveTime &&
                currentDate.getTime() - this.lastReceiveTime.getTime() > this.MaxTimeout
            ) {
                // info('more than 30s do not receive data');
            }

            let pingTimeout = currentDate.getTime() - this.lastSendTime.getTime() - this.MaxTimeout;
            if (pingTimeout < 0) {
                return;
            }

            this.ws?.send(this.message(this.terminalId, 'PING', ''));
        }, 25 * 1000);
    }

    private onWebsocketErr(e: ErrorEvent, term: Terminal) {
        term.writeln('Connection websocket error');
        console.log('Connection websocket error');
        fireEvent(new Event('CLOSE'));
        this.handleError(e);
    }

    private onWebsocketClose(e: CloseEvent, term: Terminal) {
        term.writeln('Connection websocket closed');
        fireEvent(new Event('CLOSE'));
        this.handleError(e);
    }

    private onWebsocketMessage(e: MessageEvent, term: Terminal) {
        const enableRzSz = this.enableZmodem;
        // const { info } = useLogger();
        this.lastReceiveTime = new Date();

        if (typeof e.data === 'object') {
            if (enableRzSz) {
                this.zsentry.consume(e.data);
            } else {
                this.writeBufferToTerminal(e.data);
            }
        } else {
            // info(typeof e.data);
            console.log(term);
            term.writeln('Connection websocket closed');
            this.dispatch(e.data);
        }
    }

    public isWsActivated() {
        if (this.ws) {
            return !(
                this.ws.readyState === WebSocket.CLOSING || this.ws.readyState === WebSocket.CLOSED
            );
        }
        return false;
    }

    public connectWs(url: string, term: Terminal, zsentry: any) {
        this.zsentry = zsentry;
        if (this.isWsActivated() && this.ws) {
            message.info('try to reconnect to server');
            this.ws.onerror = null;
            this.ws.onclose = null;
            this.ws.onmessage = null;
            this.ws.close();
        }

        this.term = term;

        this.ws = new WebSocket(url, ['JMS-KOKO']);
        this.ws.binaryType = 'arraybuffer';
        this.ws.onopen = () => this.onWebsocketOpen();
        this.ws.onerror = (e: ErrorEvent) => this.onWebsocketErr(e, term);
        this.ws.onclose = (e: CloseEvent) => this.onWebsocketClose(e, term);
        this.ws.onmessage = (e: MessageEvent) => this.onWebsocketMessage(e, term);
    }
}

export default WebSocketManager;
