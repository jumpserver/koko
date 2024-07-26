// import { useI18n } from 'vue-i18n';
import { fireEvent } from '@/utils';
import { Terminal } from '@xterm/xterm';
import { createDiscreteApi } from 'naive-ui';
import { useLogger } from '@/hooks/useLogger.ts';

import { handleError, writeBufferToTerminal, sendEventToLuna, formatMessage, updateIcon } from './index';
import ZmodemBrowser from 'nora-zmodemjs/src/zmodem_browser';
import { FitAddon } from '@xterm/addon-fit';

const { debug } = useLogger('WebSocketManager');
const { message } = createDiscreteApi(['message']);

class WebSocketManager {
  public wsURL: string;
  public terminal: Terminal;
  public ws: WebSocket | null = null;
  public zsentry: ZmodemBrowser.Sentry | null;
  public enableRzSz: boolean = false;
  public zmodemStatus: boolean = false;
  public terminalId: string = '';
  public zmodemStart: string = 'ZMODEM_START';
  public zmodemEnd: string = 'ZMODEM_END';
  public fitAddon: FitAddon | null = null;
  public code: any = '';
  public setting: any;

  public lastReceiveTime: Date = new Date();

  constructor(
    wsURL: string,
    terminal: Terminal,
    enableZmodem: boolean,
    zsentry: ZmodemBrowser.Sentry,
    fitAddon: FitAddon,
    code: any
  ) {
    this.wsURL = wsURL;
    this.terminal = terminal;
    this.enableRzSz = enableZmodem;
    this.zsentry = zsentry;
    this.fitAddon = fitAddon;
    this.code = code;
  }

  private onWebsocketOpen() {}

  private onWebsocketMessage(e: MessageEvent) {
    this.lastReceiveTime = new Date();

    if (typeof e.data === 'object') {
      if (this.enableRzSz) {
        this.zsentry?.consume(e.data);
      } else {
        writeBufferToTerminal(this.enableRzSz, this.zmodemStatus, e.data, this.terminal);
      }
    } else {
      debug(typeof e.data);
      this.dispatch(e.data);
    }
  }

  public dispatch(data: any) {
    // const { t } = useI18n();

    if (data === undefined) return;
    let msg = JSON.parse(data);
    debug('------------------', data);

    switch (msg.type) {
      case 'CONNECT': {
        this.terminalId = msg.id;
        try {
          this.fitAddon && this.fitAddon.fit();
        } catch (e) {
          console.log(e);
        }
        const data = {
          cols: this.terminal.cols,
          rows: this.terminal.rows,
          code: this.code
        };
        const info = JSON.parse(msg.data);
        this.setting = info.setting;
        debug(info.user);
        updateIcon(this.setting);
        this.ws && this.ws.send(formatMessage(this.terminalId, 'TERMINAL_INIT', JSON.stringify(data)));
        break;
      }
      case 'CLOSE':
        this.terminal.writeln('Receive Connection closed');
        this.ws && this.ws.close();
        sendEventToLuna('CLOSE', '');
        break;
      case 'PING':
        break;
      case 'TERMINAL_ACTION': {
        const action = msg.data;
        switch (action) {
          case this.zmodemStart:
            this.zmodemStatus = true;
            if (!this.enableRzSz) {
              // 等待用户 rz sz 文件传输
              // message.info(t('WaitFileTransfer'));
              message.info('WaitFileTransfer');
            }
            break;
          case this.zmodemEnd:
            if (!this.enableRzSz && this.zmodemStatus) {
              // message.info(t('EndFileTransfer'));
              message.info('EndFileTransfer');
              this.terminal.write('\r\n');
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
        this.terminal.writeln(errMsg);
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
              const errNotify = `'SyncUserPreferenceFailed': ${errMsg}`;
              message.error(errNotify);
            }
            break;
          default:
            debug('unknown: ', eventName);
        }
        break;
      }
      default:
        debug('default: ', data);
    }
  }

  public connectWs() {
    this.ws = new WebSocket(this.wsURL, ['JMS-KOKO']);
    this.ws.binaryType = 'arraybuffer';
    this.ws.onopen = this.onWebsocketOpen;
    this.ws.onerror = (e: Event) => {
      this.terminal.write('Connection Websocket Error');
      fireEvent(new Event('CLOSE', {}));
      handleError(e);
    };
    this.ws.onclose = (e: Event) => {
      this.terminal.write('Connection WebSocket Closed');
      fireEvent(new Event('CLOSE', {}));
      handleError(e);
    };
    this.ws.onmessage = (e: MessageEvent) => this.onWebsocketMessage(e);

    return this.ws;
  }
}

export default WebSocketManager;
