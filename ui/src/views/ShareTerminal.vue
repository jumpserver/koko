<template>
  <el-container :style="backgroundColor">
    <el-main>
      <Terminal v-if="!codeDialog" ref='term' v-bind:connectURL="wsURL" v-bind:shareCode="shareCode"
                v-on:background-color="onThemeBackground"
                v-on:event="onEvent"
                v-on:ws-data="onWsData"></Terminal>
    </el-main>

    <RightPanel ref="panel">
      <Settings :settings="settings" :title="$t('Terminal.Settings')" />
    </RightPanel>

    <ThemeConfig :visible.sync="dialogVisible" @setTheme="handleChangeTheme"></ThemeConfig>
    <el-dialog
        title="提示"
        :visible.sync="codeDialog"
        :close-on-press-escape="false"
        :close-on-click-modal="false"
        :show-close="false"
        width="30%">
      <el-form ref="form" @submit.native.prevent>
        <el-form-item :label="this.$t('Terminal.VerifyCode')">
          <el-input v-model="code"></el-input>
        </el-form-item>
      </el-form>
      <div slot="footer">
        <el-button class="item-button" @click="submitCode">{{ this.$t('Terminal.ConfirmBtn') }}</el-button>
      </div>
    </el-dialog>
  </el-container>
</template>

<script>
import Terminal from '@/components/Terminal'
import ThemeConfig from "@/components/ThemeConfig";
import {BASE_WS_URL, canvasWaterMark, defaultTheme} from "@/utils/common";
import RightPanel from '@/components/RightPanel';
import Settings from '@/components/Settings';

export default {
  components: {
    Terminal,
    ThemeConfig,
    RightPanel,
    Settings,
  },
  name: "ShareTerminal",
  data() {
    return {
      dialogVisible: false,
      themeBackground: "#1f1b1b",
      code: '',
      codeDialog: true,
      onlineUsersMap: {},
      terminalId: '',
    }
  },
  computed: {
    wsURL() {
      return this.getConnectURL()
    },
    shareCode() {
      return this.code
    },
    backgroundColor() {
      return {
        background: this.themeBackground
      }
    },
    settings() {
      const settings = [
        {
          title: this.$t('Terminal.ThemeConfig'),
          icon: 'el-icon-orange',
          disabled: () => true,
          click: () => (this.dialogVisible = !this.dialogVisible),
        },
        {
          title: this.$t('Terminal.User'),
          icon: 'el-icon-s-custom',
          disabled: () => Object.keys(this.onlineUsersMap).length > 1,
          content: Object.values(this.onlineUsersMap).map(item => {
            item.name = (this.terminalId !== item.terminal_id)?item.user:item.user + ' ['+ this.$t('Terminal.Self')+']'
            item.faIcon = item.writable?'fa-solid fa-keyboard':'fa-solid fa-eye'
            item.iconTip = item.writable?this.$t('Terminal.Writable'):this.$t('Terminal.ReadOnly')
            return item
          }).sort((a, b) => new Date(a.created) - new Date(b.created)),
          itemClick: () => {}
        }
      ]
      return settings
    }
  },
  methods: {
    getConnectURL() {
      const params = this.$route.params
      const requireParams = new URLSearchParams();
      requireParams.append('type', "share");
      requireParams.append('target_id', params.id);
      return BASE_WS_URL + "/koko/ws/terminal/?" + requireParams.toString()
    },
    onThemeBackground(val) {
      this.themeBackground = val
    },
    onWsData(msgType, msg) {
      switch (msgType) {
        case "TERMINAL_SHARE_JOIN": {
          const data = JSON.parse(msg.data);
          const key = data.terminal_id
          this.$set(this.onlineUsersMap, key, data);
          this.$log.debug(this.onlineUsersMap);
          if (this.terminalId === key) {
            this.$log.debug("self join")
            break
          }
          const joinMsg = `${data.user} ${this.$t('Terminal.JoinShare')}`
          this.$message(joinMsg)
          break
        }
        case 'TERMINAL_SHARE_LEAVE': {
          const data = JSON.parse(msg.data);
          const key = data.terminal_id;
          this.$delete(this.onlineUsersMap, key);
          const leaveMsg = `${data.user} ${this.$t('Terminal.LeaveShare')}`
          this.$message(leaveMsg)
          break
        }
        case 'TERMINAL_SHARE_USERS': {
          const data = JSON.parse(msg.data);
          this.onlineUsersMap = data;
          this.$log.debug(data);
          break
        }
        case 'TERMINAL_RESIZE': {
          const data = JSON.parse(msg.data);
          this.resize(data);
          break
        }
        case 'TERMINAL_SHARE_USER_REMOVE': {
          const data = JSON.parse(msg.data);
          this.$log.debug(data);
          this.$message(this.$t('Terminal.RemoveShareUser'))
          this.$refs.term.ws.close();
          break
        }
        case 'TERMINAL_SESSION': {
          this.terminalId = msg.id;
          const sessionInfo = JSON.parse(msg.data);
          const sessionDetail = sessionInfo.session;
          const user = this.$refs.term.currentUser;
          const username = `${user.name}(${user.username})`
          const waterMarkContent = `${username}\n${sessionDetail.asset}`
          const setting  = this.$refs.term.setting;
          if (setting.SECURITY_WATERMARK_ENABLED) {
            canvasWaterMark({
              container: document.body,
              content: waterMarkContent
            })
          }
          break
        }
        case 'TERMINAL_SESSION_PAUSE': {
          const data = JSON.parse(msg.data);
          const notifyMsg = `${data.user} ${this.$t('Terminal.PauseSession')}`
          this.$message(notifyMsg)
          break
        }
        case 'TERMINAL_SESSION_RESUME': {
          const data = JSON.parse(msg.data);
          const notifyMsg  = `${data.user} ${this.$t('Terminal.ResumeSession')}`
          this.$message(notifyMsg)
          break
        }
        default:
          break
      }
      this.$log.debug("on ws data: ", msg)
    },
    submitCode() {
      if (this.code === '') {
        this.$message(this.$t("Message.InputVerifyCode"))
        return
      }
      this.$log.debug("code:", this.code)
      this.codeDialog = false
    },
    resize({Width, Height}) {
      if (this.$refs.term && this.$refs.term.term) {
        this.$log.debug(Width, Height)
        this.$refs.term.term.resize(Width, Height)
      }
    },
    handleChangeTheme(val) {
      const themeColors = val || defaultTheme;
      if (this.$refs.term && this.$refs.term.term) {
        this.$refs.term.term.setOption("theme", themeColors);
        this.themeBackground = themeColors.background;
      }
      this.$log.debug(val);
    },
    onEvent(event, data) {
      switch (event) {
        case 'open':
          this.$log.debug("open: ", data);
          this.$refs.panel.toggle()
          break
      }
    }
  },

}
</script>

<style scoped>
.el-menu-item.is-active {
  color: #fff;
}

.item-button {
  background-color: #343333;
  color: #faf7f7;
}

.item-button:hover {
  background: rgb(134, 133, 133);
}

</style>
