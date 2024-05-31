<template>
  <div>
    <div :id="k8sId"></div>
    <el-dialog
        :title="this.$t('Terminal.UploadTitle')"
        :visible.sync="zmodeDialogVisible"
        :close-on-press-escape="false"
        :close-on-click-modal="false"
        :show-close="false"
        align-center>
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
import {bytesHuman, defaultTheme} from '@/utils/common'
import xtermTheme from "xterm-theme";

const zmodemStart = 'ZMODEM_START'
const zmodemEnd = 'ZMODEM_END'
const MAX_TRANSFER_SIZE = 1024 * 1024 * 500 // 默认最大上传下载500M
// const MAX_TRANSFER_SIZE = 1024 * 1024  // 测试 上传下载最大size 1M

const AsciiDel = 127
const AsciiBackspace = 8
const AsciiCtrlC = 3
const AsciiCtrlZ = 26

export default {
  name: "KubernetesTerminal",
  props: {
    enableZmodem: {
      type: Boolean,
      default: false,
    },
    themeName: {
      type: String,
      default: 'Default',
    },
    ws: WebSocket,
    connectInfo: Object,
    k8sId: String,
    namespace: String,
    pod: String,
    container: String,
    messages: {
      type: [Object, String],
      default: () => {},
    },
  },
  data() {
    return {
      term: null,
      fitAddon: null,
      config: null,
      zmodeDialogVisible: false,
      zmodeSession: null,
      fileList: [],
      enableRzSz: this.enableZmodem,
      zmodemStatus: false,
      termSelectionText: '',
      currentUser: null,
      setting: null,
      origin: null,
    }
  },
  watch: {
    messages: {
      immediate: true,
      handler(msg) {
        if (!msg) {
          return
        }
        this.handleMessage(msg);
      }
    }
  },
  mounted: function () {
    this.connect()
    this.updateTheme(this.themeName)
    this.initTerminal()
  },
  methods: {
    updateTheme(themeName) {
      const theme = xtermTheme[themeName] || defaultTheme
      this.term.setOption("theme", theme)
      this.$log.debug("theme: ", themeName)
      this.$emit("background-color", theme.background)
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
        },
        scrollback: 5000
      });
      const fitAddon = new FitAddon();
      term.loadAddon(fitAddon);
      const termRef = document.getElementById(this.k8sId);
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
      });
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
            this.sendData(text);
          })
          $event.preventDefault();
        } else if (this.termSelectionText !== "") {
          this.sendData(this.termSelectionText);
          $event.preventDefault();
        }
      })
      return term
    },

    connect() {
      this.config = this.loadConfig();
      this.term = this.createTerminal();

      this.zsentry = new ZmodemBrowser.Sentry({
        to_terminal: (octets) => {
          if (this.zsentry && !this.zsentry.get_confirmed_session()) {
            this.term.write(octets);
          }
        },
        sender: (octets) => {
          if (!this.wsIsActivated()) {
            this.$log.debug("websocket closed")
            return
          }
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
        if ((!this.enableZmodem) && this.zmodemStatus) {
          this.$log.debug("未开启zmodem 且当前在zmodem状态，不允许输入")
          return;
        }
        this.$log.debug("term on data event")
        data = this.preprocessInput(data)
        this.sendData(data);
      });

      this.term.onResize(({cols, rows}) => {
        const resizeData = JSON.stringify({cols, rows});
        this.sendData(resizeData, 'TERMINAL_K8S_RESIZE');
      })
    },

    base64ToUint8Array(base64) {
      const binaryString = atob(base64);
      const len = binaryString.length;
      const bytes = new Uint8Array(len);
      for (let i = 0; i < len; i++) {
        bytes[i] = binaryString.charCodeAt(i);
      }
      return bytes;
    },

    handleMessage(msg) {
      switch (msg.type) {
        case 'TERMINAL_K8S_BINARY': {
          const data = this.base64ToUint8Array(msg.raw);
          this.zsentry.consume(data);
          break
        }
        case 'TERMINAL_ACTION': {
          const action = msg.data;
          switch (action) {
            case zmodemStart:
              this.zmodemStatus = true
              if (!this.enableZmodem) {
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
          const errMsg = msg.err;
          this.$message.error(errMsg);
          this.term.writeln(errMsg);
          break
        }
        default:
          this.$log.debug("default: ", msg.data)
      }
    },

    sendData(data, type = 'TERMINAL_K8S_DATA') {
      this.$emit('send-data', {
        k8s_id: this.k8sId,
        namespace: this.namespace,
        pod: this.pod,
        container: this.container,
        type: type,
        data: data
      });
    },

    initTerminal() {
      try {
        this.fitAddon.fit();
      } catch (e) {
        console.log(e)
      }
      const data = {
        cols: this.term.cols,
        rows: this.term.rows,
        code: this.code,
      }

      this.currentUser = this.connectInfo.user;
      this.setting = this.connectInfo.setting;
      this.$log.debug(this.currentUser);
      this.updateIcon();
      this.sendData(JSON.stringify(data), 'TERMINAL_K8S_INIT');
    },

    preprocessInput(data) {
      if (this.config.backspaceAsCtrlH === "1") {
        if (data.charCodeAt(0) === AsciiDel) {
          data = String.fromCharCode(AsciiBackspace)
          this.$log.debug("backspaceAsCtrlH enabled")
        }
      }
      if (this.config.ctrlCAsCtrlZ === "1") {
        if (data.charCodeAt(0) === AsciiCtrlC) {
          data = String.fromCharCode(AsciiCtrlZ)
          this.$log.debug("ctrlCAsCtrlZ enabled")
        }
      }
      return data
    },

    handleFileChange(file, fileList) {
      if (fileList.length > 1) {
        fileList.shift()
      }
      this.$log.debug(file, fileList)
      this.fileList = fileList
    },

    updateSendProgress(xfer, percent) {
      let detail = xfer.get_details();
      let name = detail.name;
      let total = detail.size;
      percent = Math.round(percent);
      let msg = this.$t('Terminal.Upload') + ' ' + name + ': ' + bytesHuman(total) + ' ' + percent + "%"
      this.term.write("\r" + msg);
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

    loadLunaConfig() {
      let config = {};
      let fontSize = 14;
      let quickPaste = "0";
      let backspaceAsCtrlH = "0";
      let localSettings = localStorage.getItem('LunaSetting')
      if (localSettings !== null) {
        let settings = JSON.parse(localSettings)
        let commandLine = settings['command_line']
        if (commandLine) {
          fontSize = commandLine['character_terminal_font_size']
          quickPaste = commandLine['is_right_click_quickly_paste'] ? "1" : "0"
          backspaceAsCtrlH = commandLine['is_backspace_as_ctrl_h'] ? "1" : "0"
        }
      }
      if (!fontSize || fontSize < 5 || fontSize > 50) {
        fontSize = 13;
      }
      config['fontSize'] = fontSize;
      config['quickPaste'] = quickPaste;
      config['backspaceAsCtrlH'] = backspaceAsCtrlH;
      config['ctrlCAsCtrlZ'] = '0';
      return config
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

    handleReceiveSession(zsession) {
      zsession.on('offer', xfer => {
        const buffer = [];
        const detail = xfer.get_details();
        if (detail.size >= MAX_TRANSFER_SIZE) {
          const msg = this.$t("Terminal.ExceedTransferSize") + ": " + bytesHuman(MAX_TRANSFER_SIZE)
          this.$log.debug(msg)
          this.$message(msg)
          xfer.skip();
          return
        }
        xfer.on('input', payload => {
          this.updateReceiveProgress(xfer);
          buffer.push(new Uint8Array(payload));
        });
        xfer.accept().then(() => {
          this.saveToDisk(xfer, buffer);
          this.$message(this.$t("Terminal.DownloadSuccess") + " " + detail.name)
          this.term.write("\r\n")
          this.zmodeSession.abort();
        }, console.error.bind(console));
      });
      zsession.on('session_end', () => {
        this.term.write('\r\n')
      });
      zsession.start();
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
