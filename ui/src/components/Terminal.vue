<template>
  <div>
    <div id="term"></div>
    <el-dialog
        :title="this.$t('Terminal.UploadTitle')"
        :visible.sync="zmodeDialogVisible"
        :close-on-press-escape="false"
        :close-on-click-modal="false"
        :show-close="false"
        center>
      <el-row type="flex" justify="center">
        <el-upload drag action="#" :auto-upload="false" :multiple="false" ref="upload"
                   :on-change="handleFileChange">
          <i class="el-icon-upload"></i>
          <div class="el-upload__text">{{ this.$t('Terminal.UploadTips') }}</div>
        </el-upload>
      </el-row>
      <div slot="footer">
        <el-button @click="closeZmodemDialog">{{ this.$t('Terminal.Cancel') }}</el-button>
        <el-button type="primary" @click="uploadSubmit">{{ this.$t('Terminal.Upload') }}</el-button>
      </div>
    </el-dialog>
  </div>

</template>

<script>
import 'xterm/css/xterm.css'
import {Terminal} from 'xterm';
import {FitAddon} from 'xterm-addon-fit';
import ZmodemBrowser from "nora-zmodemjs/src/zmodem_browser";
import {bytesHuman, decodeToStr, fireEvent} from '@/utils/common'
import xtermTheme from "xterm-theme";

const MaxTimeout = 30 * 1000

const zmodemStart = 'ZMODEM_START'
const zmodemEnd = 'ZMODEM_END'
const MAX_TRANSFER_SIZE = 1024 * 1024 * 500 // 默认最大上传下载500M
// const MAX_TRANSFER_SIZE = 1024 * 1024  // 测试 上传下载最大size 1M

const AsciiDel = 127
const AsciiBackspace = 8

export default {
  name: "Terminal",
  props: {
    connectURL: String,
    shareCode: String,
    enableZmodem: {
      type: Boolean,
      default: false,
    }
  },
  data() {
    return {
      wsURL: this.connectURL,
      term: null,
      fitAddon: null,
      ws: null,
      pingInterval: null,
      lastReceiveTime: null,
      lastSendTime: null,
      config: null,
      zmodeDialogVisible: false,
      zmodeSession: null,
      fileList: [],
      code: this.shareCode,
      enableRzSz: this.enableZmodem,
      zmodemStatus: false,
      termSelectionText: '',
      currentUser: null,
      setting: null,
      lunaId: null,
      origin: null,
    }
  },
  mounted: function () {
    this.registerJMSEvent()
    this.connect()
    this.updateTheme()
  },
  methods: {
    updateTheme() {
      const ThemeName = window.localStorage.getItem("themeName") || null
      if (ThemeName) {
        const theme = xtermTheme[ThemeName]
        this.term.setOption("theme", theme);
        this.$log.debug("theme: ", ThemeName)
        this.$emit("background-color", theme.background)
      }
    },
    createTerminal() {
      let lineHeight = this.config.lineHeight;
      let fontSize = this.config.fontSize;
      const term = new Terminal({
        fontFamily: 'monaco, Consolas, "Lucida Console", monospace',
        lineHeight: lineHeight,
        fontSize: fontSize,
        rightClickSelectsWord: true,
        theme: {
          background: '#1f1b1b'
        }
      });
      const fitAddon = new FitAddon();
      term.loadAddon(fitAddon);
      const termRef = document.getElementById("term")
      term.open(termRef);
      fitAddon.fit();
      term.focus();
      this.fitAddon = fitAddon;
      window.addEventListener('resize', () => {
        this.fitAddon.fit();
        this.$log.debug("Windows resize event", term.cols, term.rows, term)
      })
      termRef.addEventListener('mouseenter', () => {
        term.focus();
      })
      term.onSelectionChange(() => {
        document.execCommand('copy');
        this.$log.debug("select change")
        this.termSelectionText = term.getSelection().trim();
      });
      term.attachCustomKeyEventHandler((e) => {
        if (e.ctrlKey && e.key === 'c' && term.hasSelection()) {
          return false;
        }
        return !(e.ctrlKey && e.key === 'v');
      });
      termRef.addEventListener('contextmenu', ($event) => {
        if ($event.ctrlKey || this.config.quickPaste !== '1') {
          return;
        }
        if (navigator.clipboard && navigator.clipboard.readText) {
          navigator.clipboard.readText().then((text) => {
            if (this.wsIsActivated()) {
              this.ws.send(this.message(this.terminalId, 'TERMINAL_DATA', text))
            }
          })
          $event.preventDefault();
        } else if (this.termSelectionText !== "") {
          if (this.wsIsActivated()) {
            this.ws.send(this.message(this.terminalId, 'TERMINAL_DATA', this.termSelectionText))
          }
          $event.preventDefault();
        }
      })
      return term
    },
    registerJMSEvent() {
      window.addEventListener("message", this.handleEventFromLuna, false);
    },

    handleEventFromLuna(evt) {
      const msg = evt.data;
      switch (msg.name) {
        case 'PING':
          if (this.lunaId != null) {
            return
          }
          this.lunaId = msg.id;
          this.origin = evt.origin;
          this.sendEventToLuna('PONG', '');
          break
        case 'CMD':
          this.sendDataFromWindow(msg.data)
          break
        case 'FOCUS':
          if (this.term) {
            this.term.focus()
          }
          break
      }
      console.log('KoKo got post message: ', msg)
    },

    sendEventToLuna(name, data) {
      if (this.lunaId != null) {
        window.parent.postMessage({name: name, id: this.lunaId, data: data}, this.origin)
      }
    },

    connect() {
      this.$log.debug(this.wsURL)
      const ws = new WebSocket(this.wsURL, ["JMS-KOKO"]);
      this.config = this.loadConfig();
      this.term = this.createTerminal();
      this.$log.debug(ZmodemBrowser);
      this.zsentry = new ZmodemBrowser.Sentry({
        to_terminal: (octets) => {
          if (this.zsentry && !this.zsentry.get_confirmed_session()) {
            this.term.write(decodeToStr(octets));
          }
        },
        sender: (octets) => {
          if (!this.wsIsActivated()) {
            this.$log.debug("websocket closed")
            return
          }
          this.lastSendTime = new Date();
          this.ws.send(new Uint8Array(octets));
        },
        on_retract: () => {
          console.log('zmodem Retract')
        },
        on_detect: (detection) => {
          const zsession = detection.confirm();
          this.term.write("\r\n")
          if (zsession.type === "send") {
            this.handleSendSession(zsession);
          } else {
            this.handleReceiveSession(zsession);
          }
        }
      });

      this.term.onData(data => {
        if (!this.wsIsActivated()) {
          this.$log.debug("websocket closed")
          return
        }
        if ((!this.enableZmodem) && this.zmodemStatus) {
          this.$log.debug("未开启zmodem 且当前在zmodem状态，不允许输入")
          return;
        }
        this.lastSendTime = new Date();
        this.$log.debug("term on data event")
        data = this.preprocessInput(data)
        this.ws.send(this.message(this.terminalId, 'TERMINAL_DATA', data));
      });

      this.term.onResize(({cols, rows}) => {
        if (!this.wsIsActivated()) {
          return
        }
        this.$log.debug("send term resize ")
        this.ws.send(this.message(this.terminalId, 'TERMINAL_RESIZE', JSON.stringify({cols, rows})))
      })
      this.ws = ws;
      ws.binaryType = "arraybuffer";
      ws.onopen = this.onWebsocketOpen;
      ws.onerror = this.onWebsocketErr;
      ws.onclose = this.onWebsocketClose;
      ws.onmessage = this.onWebsocketMessage;
      window.SendTerminalData = this.sendDataFromWindow;
    },

    onWebsocketMessage(e) {
      this.lastReceiveTime = new Date();
      if (typeof e.data === 'object') {
        if (this.enableRzSz) {
          this.zsentry.consume(e.data);
        } else {
          this.writeBufferToTerminal(e.data);
        }
      } else {
        this.$log.debug(typeof e.data)
        this.dispatch(e.data);
      }
    },

    writeBufferToTerminal(data) {
      if ((!this.enableZmodem) && this.zmodemStatus) {
        this.$log.debug("未开启zmodem 且当前在zmodem状态，不允许显示")
        return;
      }
      this.term.write(decodeToStr(data));
    },

    onWebsocketOpen() {
      if (this.pingInterval !== null) {
        clearInterval(this.pingInterval);
      }
      this.lastReceiveTime = new Date();
      this.pingInterval = setInterval(() => {
        if (this.ws.readyState === WebSocket.CLOSING ||
            this.ws.readyState === WebSocket.CLOSED) {
          clearInterval(this.pingInterval)
          return
        }
        let currentDate = new Date();
        if ((this.lastReceiveTime - currentDate) > MaxTimeout) {
          this.$log.debug("more than 30s do not receive data")
        }
        let pingTimeout = (currentDate - this.lastSendTime) - MaxTimeout
        if (pingTimeout < 0) {
          return;
        }
        this.ws.send(this.message(this.terminalId, 'PING', ""));
      }, 25 * 1000);
    },

    onWebsocketErr(e) {
      this.term.writeln("Connection websocket error");
      fireEvent(new Event("CLOSE", {}))
      this.handleError(e)
    },

    onWebsocketClose(e) {
      this.term.writeln("Connection websocket closed");
      fireEvent(new Event("CLOSE", {}))
      this.handleError(e)
    },

    sendDataFromWindow(data) {
      if (!this.wsIsActivated()) {
        this.$log.debug("ws disconnected")
        return
      }
      if (this.enableZmodem && (!this.zmodemStatus)) {
        this.ws.send(this.message(this.terminalId, 'TERMINAL_DATA', data));
        this.$log.debug('send data from window')
      }
    },

    dispatch(data) {
      if (data === undefined) {
        return
      }
      let msg = JSON.parse(data)
      switch (msg.type) {
        case 'CONNECT': {
          this.terminalId = msg.id;
          try {
            this.fitAddon.fit();
          }catch (e){
           console.log(e)
          }
          const data = {
            cols: this.term.cols,
            rows: this.term.rows,
            code: this.code
          }
          const info = JSON.parse(msg.data);
          this.currentUser = info.user;
          this.setting = info.setting;
          this.$log.debug(this.currentUser);
          this.updateIcon();
          this.ws.send(this.message(this.terminalId, 'TERMINAL_INIT',
              JSON.stringify(data)));
          break
        }
        case "CLOSE":
          this.term.writeln("Receive Connection closed");
          this.ws.close();
          this.sendEventToLuna('CLOSE', '')
          break
        case "PING":
          break
        case 'TERMINAL_ACTION': {
          const action = msg.data;
          switch (action) {
            case zmodemStart:
              this.zmodemStatus = true
              if (!this.enableZmodem) {
                // 等待用户 rz sz 文件传输
                this.$message(this.$t("Terminal.WaitFileTransfer"));
              }
              break
            case zmodemEnd:
              if (!this.enableZmodem && this.zmodemStatus) {
                this.$message(this.$t("Terminal.EndFileTransfer"));
                this.term.write("\r\n")
              }
              this.zmodemStatus = false
              break
            default:
              this.zmodemStatus = false
          }
          break
        }
        case 'TERMINAL_ERROR': {
          const errMsg = msg.data;
          this.$message(errMsg);
          break
        }
        default:
          this.$log.debug("default: ", data)
      }
      this.$emit('ws-data', msg.type, msg)
    },

    wsIsActivated() {
      if (this.ws) {
        return !(this.ws.readyState === WebSocket.CLOSING ||
            this.ws.readyState === WebSocket.CLOSED)
      }
      return false
    },

    message(id, type, data) {
      return JSON.stringify({
        id,
        type,
        data,
      });
    },

    handleError(e) {
      console.log(e)
    },

    updateIcon() {
      const faviconURL = this.setting['LOGO_URLS']?.favicon
      let link = document.querySelector("link[rel*='icon']")
      if (!link) {
        link = document.createElement('link')
        link.type = 'image/x-icon'
        link.rel = 'shortcut icon'
        document.getElementsByTagName('head')[0].appendChild(link)
      }
      if (faviconURL) {
        link.href = faviconURL
      }
    },

    loadLunaConfig() {
      let config = {};
      let fontSize = 14;
      let quickPaste = "0";
      let backspaceAsCrtlH = "0";
      // localStorage.getItem default null
      let localSettings = localStorage.getItem('LunaSetting')
      if (localSettings !== null) {
        let settings = JSON.parse(localSettings)
        fontSize = settings['fontSize']
        quickPaste = settings['quickPaste']
        backspaceAsCrtlH = settings['backspaceAsCrtlH']
      }
      if (!fontSize || fontSize < 5 || fontSize > 50) {
        fontSize = 13;
      }
      config['fontSize'] = fontSize;
      config['quickPaste'] = quickPaste;
      config['backspaceAsCrtlH'] = backspaceAsCrtlH;
      return config
    },

    loadConfig() {
      const config = this.loadLunaConfig();
      const ua = navigator.userAgent.toLowerCase();
      let lineHeight = 1;
      if (ua.indexOf('windows') !== -1) {
        lineHeight = 1.2;
      }
      config['lineHeight'] = lineHeight
      return config
    },

    handleFileChange(file, fileList) {
      if (fileList.length > 1) {
        fileList.shift()
      }
      this.$log.debug(file, fileList)
      this.fileList = fileList
    },

    handleReceiveSession(zsession) {
      zsession.on('offer', xfer => {
        const buffer = [];
        const detail = xfer.get_details();
        xfer.on('input', payload => {
          this.updateReceiveProgress(xfer);
          buffer.push(new Uint8Array(payload));
        });
        xfer.accept().then(() => {
          this.saveToDisk(xfer, buffer);
          this.$message(this.$t("Terminal.DownloadSuccess") + " " + detail.name)
          this.term.write("\r\n")
        }, console.error.bind(console));
      });
      zsession.on('session_end', () => {
        this.term.write('\r\n')
      });
      zsession.start();
    },

    saveToDisk(xfer, buffer) {
      ZmodemBrowser.Browser.save_to_disk(buffer, xfer.get_details().name);
    },
    updateReceiveProgress(xfer) {
      let detail = xfer.get_details();
      let name = detail.name;
      let total = detail.size;
      let offset = xfer.get_offset();
      let percent;
      if (total === 0 || total === offset) {
        percent = 100
      } else {
        percent = Math.round(offset / total * 100);
      }
      let msg = this.$t('Terminal.Download') + ' ' + name + ': ' + bytesHuman(total) + ' ' + percent + "%"
      this.term.write("\r" + msg);
    },
    updateSendProgress(xfer, percent) {
      let detail = xfer.get_details();
      let name = detail.name;
      let total = detail.size;
      percent = Math.round(percent);
      let msg = this.$t('Terminal.Upload') + ' ' + name + ': ' + bytesHuman(total) + ' ' + percent + "%"
      this.term.write("\r" + msg);
    },
    handleSendSession(zsession) {
      this.zmodeSession = zsession;
      this.zmodeDialogVisible = true;

      zsession.on('session_end', () => {
        this.zmodeSession = null;
        this.fileList = [];
        this.term.write('\r\n')
        this.$refs.upload.clearFiles();
      });
    },

    uploadSubmit() {
      if (this.fileList.length === 0) {
        this.$message(this.$t("Terminal.MustSelectOneFile"))
        return;
      }
      if (this.fileList.length !== 1) {
        this.$message(this.$t("Terminal.MustOneFile"))
        return;
      }
      const selectFile = this.fileList[0]
      if (selectFile.size >= MAX_TRANSFER_SIZE) {
        this.$log.debug(selectFile)
        const msg = this.$t("Terminal.ExceedTransferSize") + ": " + bytesHuman(MAX_TRANSFER_SIZE)
        this.$message(msg)
        return;
      }

      this.zmodeDialogVisible = false;
      if (!this.zmodeSession) {
        return
      }
      const filesObj = this.fileList.map(el => el.raw);
      this.$log.debug("Zomdem submit file: ", filesObj)
      ZmodemBrowser.Browser.send_files(this.zmodeSession, filesObj,
          {
            on_offer_response: (obj, xfer) => {
              if (xfer) {
                xfer.on('send_progress', (percent) => {
                  this.updateSendProgress(xfer, percent)
                });
              }
            },
            on_file_complete: (obj) => {
              this.$log.debug("file_complete", obj);
              this.$message(this.$t('Terminal.UploadSuccess') + ' ' + obj.name)
            },
          }
      ).then(this.zmodeSession.close.bind(this.zmodeSession),
          console.error.bind(console)
      ).catch(err => {
        console.log(err)
      });
    },

    closeZmodemDialog() {
      this.zmodeDialogVisible = false;
      if (this.zmodeSession) {
        this.$log.debug("cancel abort")
        this.zmodeSession.abort();
      }
      this.$refs.upload.clearFiles();
      this.$log.debug("删除dialog的文件")
    },

    createShareInfo(sid, val) {
      this.sendWsMessage('TERMINAL_SHARE', {session_id: sid, expired: val,})
    },

    sendWsMessage(type, data) {
      if (this.wsIsActivated()) {
        const msg = this.message(this.terminalId, type, JSON.stringify(data))
        this.ws.send(msg)
      }
    },

    preprocessInput(data) {
      if (this.config.backspaceAsCrtlH === "1") {
        if (data.charCodeAt(0) === AsciiDel) {
          data = String.fromCharCode(AsciiBackspace)
        }
      }
      return data
    }
  }
}
</script>

<style scoped>
@import '../styles/index.css';

div {
  height: 100%;
}

#term {
  height: calc(100% - 10px);
  padding: 10px 0 10px 10px;
}
</style>