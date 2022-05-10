<template>
  <el-container :style="backgroundColor">
    <el-main>
      <Terminal ref='term'
                v-bind:enable-zmodem='true'
                v-bind:connectURL="wsURL"
                v-on:background-color="onThemeBackground"
                v-on:ws-data="onWsData"></Terminal>
    </el-main>
    <el-aside width="60px" center>
      <el-menu :collapse="true" :background-color="themeBackground" text-color="#ffffff">
        <el-menu-item @click="dialogVisible=!dialogVisible" index="0">
          <i class="el-icon-orange"></i>
          <span slot="title">{{ this.$t('Terminal.ThemeConfig') }}</span>
        </el-menu-item>
        <el-menu-item @click="shareDialogVisible=!shareDialogVisible" v-if="enableShare" index="1">
          <i class="el-icon-share"></i>
          <span slot="title">{{ this.$t('Terminal.Share') }}</span>
        </el-menu-item>
        <el-submenu index="2" v-if="displayOnlineUser">
          <template slot="title">
            <i class="el-icon-s-custom"></i>
            <span slot="title">{{ this.$t('Terminal.OnlineUsers') }}</span>
          </template>
          <el-menu-item-group>
            <span slot="title">{{ this.$t('Terminal.User') }} {{ onlineKeys.length }}</span>
            <el-menu-item v-for="(item ,key) of onlineUsersMap" :key="key">{{ item.user }}</el-menu-item>
          </el-menu-item-group>
        </el-submenu>

      </el-menu>
    </el-aside>
    <ThemeConfig :visible.sync="dialogVisible" @setTheme="handleChangeTheme"></ThemeConfig>

    <el-dialog
        :title="shareTitle"
        :visible.sync="shareDialogVisible"
        width="30%"
        :close-on-press-escape="false"
        :close-on-click-modal="false"
        @close="shareDialogClosed"
        center>
      <el-form v-if="!shareId" v-loading="loading">
        <el-form-item :label="this.$t('Terminal.ExpiredTime')">
          <el-select v-model="expiredTime" :placeholder="this.$t('Terminal.SelectAction')">
            <el-option
                v-for="item in expiredOptions"
                :key="item.value"
                :label="item.label"
                :value="item.value">
            </el-option>
          </el-select>
        </el-form-item>
        <el-form-item :label="this.$t('Terminal.ShareUser')">
            <el-select v-model="users" multiple filterable remote reserve-keyword :placeholder="this.$t('Terminal.GetShareUser')"
                :remote-method="getSessionUser" :loading="userLoading">
                <el-option
                  v-for="item in userOptions"
                  :key="item.id"
                  :label="item.name + '(' + item.username + ')'"
                  :value="item.id">
                </el-option>
              </el-select>
        </el-form-item>
      </el-form>
      <el-result v-if="shareId" icon="success" :title="this.$t('Terminal.CreateSuccess')">
        <template slot="extra">
        </template>
      </el-result>
      <el-form v-if="shareId">
        <el-form-item :label="this.$t('Terminal.LinkAddr')">
          <el-input readonly :value="shareURL"/>
        </el-form-item>
        <el-form-item :label="this.$t('Terminal.VerifyCode')">
          <el-input readonly :value="shareCode"/>
        </el-form-item>
      </el-form>
      <span slot="footer" class="dialog-footer">
    <el-button type="primary" v-if="!shareId"
               @click="handleShareURlCreated">{{ this.$t('Terminal.CreateLink') }}</el-button>
    <el-button type="primary" v-if="shareId" @click="copyShareURL">{{ this.$t('Terminal.CopyLink') }} </el-button>
  </span>
    </el-dialog>
  </el-container>
</template>

<script>
import Terminal from '@/components/Terminal';
import ThemeConfig from "@/components/ThemeConfig";
import {BASE_URL, BASE_WS_URL, CopyTextToClipboard} from "@/utils/common";

export default {
  components: {
    Terminal,
    ThemeConfig,
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
        {label: "1m", value: 1},
        {label: "5m", value: 5},
        {label: "10m", value: 10},
        {label: "20m", value: 20},
        {label: "60m", value: 60},
      ],
      shareId: null,
      loading: false,
      userLoading: false,
      shareCode: null,
      shareInfo: null,
      onlineUsersMap: {},
      onlineKeys: [],
      userOptions: [],
      users: []
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
    displayOnlineUser() {
      return this.onlineKeys.length > 1;
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
          const sessionDetail = JSON.parse(msg.data);
          this.sessionId = sessionDetail.id;
          const setting = this.$refs.term.setting;
          if (setting.SECURITY_SESSION_SHARE) {
            this.enableShare = true;
          }
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
          this.onlineUsersMap[key] = data;
          this.$log.debug(this.onlineUsersMap);
          this.updateOnlineCount();
          break
        }
        case 'TERMINAL_SHARE_LEAVE': {
          const data = JSON.parse(msg.data);
          const key = data.user_id + data.created;
          delete this.onlineUsersMap[key];
          this.updateOnlineCount();
          break
        }
        case 'TERMINAL_GET_SHARE_USER': {
          this.userLoading = false;
          const data = JSON.parse(msg.data);
          this.userOptions = data;
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
    },
    getSessionUser(query) {
      if (query !== '' && this.$refs.term) {
        this.userLoading = true;
        this.$refs.term.getUserInfo(query);
      } else {
        this.userOptions = []
      }
    }
  },
}
</script>

<style scoped>
.el-menu-item.is-active {
  color: #ffffff;
}
</style>
