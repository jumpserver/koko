<template>
  <el-container :style="backgroundColor">
    <el-main>
      <Terminal v-if="!codeDialog" ref='term' v-bind:connectURL="wsURL" v-bind:shareCode="shareCode"
                v-on:background-color="onThemeBackground"
                v-on:ws-data="onWsData"></Terminal>
    </el-main>

    <RightPanel>
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
        <el-button type="primary" @click="submitCode">{{ this.$t('Terminal.ConfirmBtn') }}</el-button>
      </div>
    </el-dialog>
  </el-container>
</template>

<script>
import Terminal from '@/components/Terminal'
import ThemeConfig from "@/components/ThemeConfig";
import {BASE_WS_URL, canvasWaterMark} from "@/utils/common";
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
      onlineKeys: [],
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
    displayOnlineUser() {
      return this.onlineKeys.length > 1;
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
            item.name = item.user
            return item
          }),
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
          const key = data.user_id + data.created;
          this.$set(this.onlineUsersMap, key, data);
          this.$log.debug(this.onlineUsersMap);
          this.updateOnlineCount();
          break
        }
        case 'TERMINAL_SHARE_LEAVE': {
          const data = JSON.parse(msg.data);
          const key = data.user_id + data.created;
          this.$delete(this.onlineUsersMap, key);
          this.updateOnlineCount();
          break
        }
        case 'TERMINAL_SHARE_USERS': {
          const data = JSON.parse(msg.data);
          this.onlineUsersMap = data;
          this.updateOnlineCount();
          this.$log.debug(data);
          break
        }
        case 'TERMINAL_RESIZE': {
          const data = JSON.parse(msg.data);
          this.resize(data);
          break
        }
        case 'TERMINAL_SESSION': {
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
      if (this.$refs.term && this.$refs.term.term) {
        this.$refs.term.term.setOption("theme", val);
        this.themeBackground = val.background;
      }
      this.$log.debug(val);
    },
    updateOnlineCount() {
      const keys = Object.keys(this.onlineUsersMap);
      this.$log.debug(keys);
      this.onlineKeys = keys;
    }
  },

}
</script>

<style scoped>
.el-menu-item.is-active {
  color: #ffffff;
}
</style>
