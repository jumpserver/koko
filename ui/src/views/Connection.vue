<template>
  <el-container :style="backgroundColor">
    <el-main>
      <Terminal ref='term'
                v-bind:enable-zmodem='true'
                v-bind:connectURL="wsURL"
                v-on:background-color="onThemeBackground"
                v-on:ws-data="onWsData"></Terminal>
    </el-main>
    <RightPanel>
      <Settings :settings="settings" :title="$t('Terminal.Settings')" />
    </RightPanel>

    <ThemeConfig :visible.sync="dialogVisible" @setTheme="handleChangeTheme"></ThemeConfig>

    <el-dialog
        :title="shareTitle"
        :visible.sync="shareDialogVisible"
        width="30%"
        :close-on-press-escape="false"
        :close-on-click-modal="false"
        @close="shareDialogClosed"
        :modal="false">
      <div v-if="!shareId">
        <el-form v-loading="loading">
          <el-form-item :label="this.$t('Terminal.ExpiredTime')">
            <el-select v-model="expiredTime" :placeholder="this.$t('Terminal.SelectAction')" style="width: 100%">
              <el-option
                  v-for="item in expiredOptions"
                  :key="item.value"
                  :label="item.label"
                  :value="item.value">
              </el-option>
            </el-select>
          </el-form-item>
        </el-form>
        <div>
          <el-button type="primary" @click="handleShareURlCreated">{{ this.$t('Terminal.CreateLink') }}</el-button>
        </div>
      </div>
      <div v-else>
        <el-result icon="success" :title="this.$t('Terminal.CreateSuccess')">
        </el-result>
        <el-form>
          <el-form-item :label="this.$t('Terminal.LinkAddr')">
            <el-input readonly :value="shareURL"/>
          </el-form-item>
          <el-form-item :label="this.$t('Terminal.VerifyCode')">
            <el-input readonly :value="shareCode"/>
          </el-form-item>
        </el-form>
        <div>
          <el-button type="primary" @click="copyShareURL">{{ this.$t('Terminal.CopyLink') }} </el-button>
        </div>
      </div>
    </el-dialog>
  </el-container>
</template>

<script>
import Terminal from '@/components/Terminal';
import ThemeConfig from "@/components/ThemeConfig";
import {BASE_URL, BASE_WS_URL, CopyTextToClipboard} from "@/utils/common";
import RightPanel from '@/components/RightPanel';
import Settings from '@/components/Settings';

export default {
  components: {
    Terminal,
    ThemeConfig,
    RightPanel,
    Settings,
  },
  name: "Connection",
  data() {
    return {
      sessionId: '',
      enableShare: false,
      dialogVisible: false,
      themeBackground: "#1f1b1b",
      shareDialogVisible: false,
      expiredTime: 10,
      expiredOptions: [
        {label: "10m", value: 10},
        {label: "20m", value: 20},
        {label: "60m", value: 60},
      ],
      shareId: null,
      loading: false,
      shareCode: null,
      shareInfo: null,
      onlineUsersMap: {},
      onlineKeys: [],
    }
  },
  computed: {
    wsURL() {
      return this.getConnectURL()
    },
    backgroundColor() {
      return {
        background: this.themeBackground
      }
    },
    shareTitle() {
      return this.shareId ? this.$t('Terminal.Share') : this.$t('Terminal.CreateLink')
    },
    shareURL() {
      return this.shareId ? this.generateShareURL() : this.$t('Terminal.NoLink')
    },
    settings() {
      const settings = [
        {
          title: this.$t('Terminal.ThemeConfig'),
          icon: 'el-icon-orange',
          disabled: () => false,
          click: () => (this.dialogVisible = !this.dialogVisible),
        },
        {
          title: this.$t('Terminal.Share'),
          icon: 'el-icon-share',
          disabled: () => !this.enableShare,
          click: () => (this.shareDialogVisible = !this.shareDialogVisible),
        },
        {
          title: this.$t('Terminal.User'),
          icon: 'el-icon-s-custom',
          disabled: () => Object.keys(this.onlineUsersMap).length < 1,
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
      let connectURL = '';
      const routeName = this.$route.name
      switch (routeName) {
        case "Token": {
          const params = this.$route.params
          const requireParams = new URLSearchParams();
          requireParams.append('type', "token");
          requireParams.append('target_id', params.id);
          connectURL = BASE_WS_URL + "/koko/ws/token/?" + requireParams.toString()
          break
        }
        case "TokenParams": {
          const urlParams = new URLSearchParams(window.location.search.slice(1));
          connectURL = `${BASE_WS_URL}/koko/ws/token/?${urlParams.toString()}`;
          break
        }
        default: {
          const urlParams = new URLSearchParams(window.location.search.slice(1));
          connectURL = `${BASE_WS_URL}/koko/ws/terminal/?${urlParams.toString()}`;
        }
      }
      return connectURL
    },
    generateShareURL() {
      return `${BASE_URL}/koko/share/${this.shareId}/`
    },
    copyShareURL() {
      if (!this.enableShare) {
        return
      }
      if (!this.shareId) {
        return;
      }
      const shareURL = this.generateShareURL();
      this.$log.debug("share URL: " + shareURL)
      const linkTitle = this.$t('Terminal.LinkAddr');
      const codeTitle = this.$t('Terminal.VerifyCode')
      const text = `${linkTitle}： ${shareURL}\n${codeTitle}: ${this.shareCode}`
      CopyTextToClipboard(text)
      this.$message(this.$t("Terminal.CopyShareURLSuccess"))
    },
    onThemeBackground(val) {
      this.themeBackground = val
    },
    onWsData(msgType, msg) {
      switch (msgType) {
        case "TERMINAL_SESSION": {
          const sessionInfo = JSON.parse(msg.data);
          const sessionDetail = sessionInfo.session;
          const perms = sessionInfo.permission;
          this.sessionId = sessionDetail.id;
          const setting = this.$refs.term.setting;
          if (setting.SECURITY_SESSION_SHARE) {
            this.enableShare = true;
          }
          this.$refs.term.updatePermission(perms.actions);
          break
        }
        case "TERMINAL_SHARE": {
          const data = JSON.parse(msg.data);
          this.shareId = data.share_id;
          this.shareCode = data.code;
          this.loading = false
          break
        }
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
        default:
          break
      }
      this.$log.debug("on ws data: ", msg)
    },

    handleChangeTheme(val) {
      if (this.$refs.term.term) {
        this.$refs.term.term.setOption("theme", val);
        this.themeBackground = val.background;
      }
      this.$log.debug(val);
    },
    handleShareURlCreated() {
      this.loading = true
      if (this.$refs.term) {
        this.$refs.term.createShareInfo(this.sessionId, this.expiredTime);
      }
      this.$log.debug("分享请求数据： ", this.expiredTime, this.sessionId)

    },
    shareDialogClosed() {
      this.$log.debug("share dialog closed")
      this.loading = false;
      this.shareId = null;
      this.shareCode = null;
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
.settings {
  padding: 24px 20px;
}

.el-result {
  padding: 0
}
</style>
