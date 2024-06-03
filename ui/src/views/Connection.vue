<template>
  <el-container :style="backgroundColor">
    <el-main>
      <Terminal ref='term'
                v-bind:enable-zmodem='true'
                v-bind:connectURL="wsURL"
                v-on:background-color="onThemeBackground"
                v-on:event="onEvent"
                v-on:ws-data="onWsData"></Terminal>
    </el-main>
    <RightPanel ref="panel">
      <Settings :settings="settings" :title="$t('Settings')"/>
    </RightPanel>

    <ThemeConfig :visible.sync="dialogVisible"  :themeName="themeName"
                 @setTheme="handleChangeTheme"
                 @syncThemeName="handleSyncTheme">
    </ThemeConfig>

    <el-dialog
        :title="shareTitle"
        :visible.sync="shareDialogVisible"
        width="30%"
        :close-on-press-escape="false"
        :close-on-click-modal="false"
        @close="shareDialogClosed"
        :modal="false"
        class="share-dialog"
        center>
      <div v-if="!shareId">
        <el-form v-loading="loading" :model="shareLinkRequest">
          <el-form-item :label="this.$t('ExpiredTime')">
            <el-select v-model="shareLinkRequest.expiredTime" :placeholder="this.$t('SelectAction')">
              <el-option
                  v-for="item in expiredOptions"
                  :key="item.value"
                  :label="item.label"
                  :value="item.value">
              </el-option>
            </el-select>
          </el-form-item>
          <el-form-item :label="this.$t('ActionPerm')">
            <el-select v-model="shareLinkRequest.actionPerm" :placeholder="this.$t('ActionPerm')">
              <el-option
                  v-for="item in actionsPermOptions"
                  :key="item.value"
                  :label="item.label"
                  :value="item.value">
              </el-option>
            </el-select>
          </el-form-item>
          <el-form-item :label="this.$t('ShareUser')">
            <el-select v-model="shareLinkRequest.users" multiple filterable remote reserve-keyword
                       :placeholder="this.$t('GetShareUser')"
                       :remote-method="getSessionUser" :loading="userLoading">
              <el-option
                  v-for="item in userOptions"
                  :key="item.id"
                  :label="item.name + '(' + item.username + ')'"
                  :value="item.id">
              </el-option>
            </el-select>
            <div style="color: #d9d1d1;font-size: 12px">{{ this.$t('ShareUserHelpText') }}</div>
          </el-form-item>
        </el-form>
        <div>
          <el-button class="share-btn" @click="handleShareURlCreated">{{ this.$t('CreateLink') }}</el-button>
        </div>
      </div>
      <div v-else>
        <el-result icon="success" class="result" :title="this.$t('CreateSuccess')">
        </el-result>
        <el-form>
          <el-form-item :label="this.$t('LinkAddr')">
            <el-input readonly :value="shareURL"/>
          </el-form-item>
          <el-form-item :label="this.$t('VerifyCode')">
            <el-input readonly :value="shareCode"/>
          </el-form-item>
        </el-form>
        <div>
          <el-button class="share-btn" @click="copyShareURL">{{ this.$t('CopyLink') }}</el-button>
        </div>
      </div>
    </el-dialog>
  </el-container>
</template>

<script>
import Terminal from '@/components/Terminal';
import ThemeConfig from "@/components/ThemeConfig";
import {BASE_URL, BASE_WS_URL, CopyTextToClipboard, defaultTheme} from "@/utils/common";
import RightPanel from '@/components/RightPanel';
import Settings from '@/components/Settings';
import xtermTheme from "xterm-theme";

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
      shareLinkRequest: {
        expiredTime: 10,
        actionPerm: 'writable',
        users: []
      },
      expiredOptions: [
        {label: this.getMinuteLabel(1), value: 1},
        {label: this.getMinuteLabel(5), value: 5},
        {label: this.getMinuteLabel(10), value: 10},
        {label: this.getMinuteLabel(20), value: 20},
        {label: this.getMinuteLabel(60), value: 60},
      ],
      actionsPermOptions: [
        {label: this.$t('Writable'), value: "writable"},
        {label: this.$t('ReadOnly'), value: "readonly"},
      ],
      shareId: null,
      loading: false,
      userLoading: false,
      shareCode: null,
      shareInfo: null,
      onlineUsersMap: {},
      userOptions: [],
      themeName: 'Default',
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
      return this.shareId ? this.$t('Share') : this.$t('CreateLink')
    },
    shareURL() {
      return this.shareId ? this.generateShareURL() : this.$t('NoLink')
    },
    settings() {
      const settings = [
        {
          title: this.$t('ThemeConfig'),
          icon: 'el-icon-orange',
          disabled: () => false,
          click: () => (this.dialogVisible = !this.dialogVisible),
        },
        {
          title: this.$t('Share'),
          icon: 'el-icon-share',
          disabled: () => !this.enableShare,
          click: () => (this.shareDialogVisible = !this.shareDialogVisible),
        },
        {
          title: this.$t('User'),
          icon: 'el-icon-s-custom',
          disabled: () => Object.keys(this.onlineUsersMap).length < 1,
          content: Object.values(this.onlineUsersMap).map(item => {
            item.name = item.user
            item.faIcon = item.writable ? 'fa-solid fa-keyboard' : 'fa-solid fa-eye'
            item.iconTip = item.writable ? this.$t('Writable') : this.$t('ReadOnly')
            return item
          }).sort((a, b) => new Date(a.created) - new Date(b.created)),
          itemClick: () => {
          },
          itemActions: [
            {
              faIcon: 'fa-solid fa-trash-can',
              tipText: this.$t('Remove'),
              style: {
                color: "#f56c6c"
              },
              hidden: (user) => {
                this.$log.debug("Remove user hidden: ", user)
                return user.primary
              },
              click: (user) => {
                if (user.primary) {
                  return
                }
                this.$confirm(this.$t('RemoveShareUserConfirm'))
                    .then(() => {
                      if (this.$refs.term) {
                        this.$refs.term.removeShareUser(this.sessionId, user)
                      }
                    })
                    .catch(() => {
                      this.$log.debug("not Remove user", user)
                    });
              }
            }
          ],
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
      const linkTitle = this.$t('LinkAddr');
      const codeTitle = this.$t('VerifyCode')
      const text = `${linkTitle}： ${shareURL}\n${codeTitle}: ${this.shareCode}`
      CopyTextToClipboard(text)
      this.$message(this.$t("CopyShareURLSuccess"))
    },
    onThemeBackground(val) {
      this.themeBackground = val
      this.$log.debug("onThemeBackground: ", val)
    },
    onWsData(msgType, msg) {
      switch (msgType) {
        case "TERMINAL_SESSION": {
          const sessionInfo = JSON.parse(msg.data);
          const sessionDetail = sessionInfo.session;
          this.$log.debug("sessionDetail backspaceAsCtrlH: ", sessionInfo.backspaceAsCtrlH);
          this.$log.debug("sessionDetail ctrlCAsCtrlZ: ", sessionInfo.ctrlCAsCtrlZ);
          this.$log.debug("sessionDetail themeName: ", sessionInfo.themeName);
          this.$log.debug("sessionDetail permissions: ", sessionInfo.permission)
          const enableShare = sessionInfo.permission.actions.includes('share');
          if ((sessionInfo.backspaceAsCtrlH !== undefined) && this.$refs.term) {
            const value = sessionInfo.backspaceAsCtrlH ? '1' : '0';
            this.$log.debug("set backspaceAsCtrlH: " + value);
            this.$refs.term.setConfig('backspaceAsCtrlH', value);
          }
          if (this.$refs.term) {
            const value = sessionInfo.ctrlCAsCtrlZ ? '1' : '0';
            this.$log.debug("set ctrlCAsCtrlZ: " + value);
            this.$refs.term.setConfig('ctrlCAsCtrlZ', value);
          }
          this.sessionId = sessionDetail.id;
          const setting = this.$refs.term.setting;
          if (setting.SECURITY_SESSION_SHARE && enableShare) {
            this.enableShare = true;
          }
          this.themeName = sessionInfo.themeName;
          this.updateThemeSetting(sessionInfo.themeName, xtermTheme[sessionInfo.themeName]);
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
          const key = data.terminal_id
          this.$set(this.onlineUsersMap, key, data);
          this.$log.debug(this.onlineUsersMap);
          if (data.primary) {
            this.$log.debug("primary user 不提醒")
            break
          }
          const joinMsg = `${data.user} ${this.$t('JoinShare')}`
          this.$message(joinMsg)
          break
        }
        case 'TERMINAL_SHARE_LEAVE': {
          const data = JSON.parse(msg.data);
          const key = data.terminal_id;
          this.$delete(this.onlineUsersMap, key);
          const leaveMsg = `${data.user} ${this.$t('LeaveShare')}`
          this.$message(leaveMsg)
          break
        }
        case 'TERMINAL_GET_SHARE_USER': {
          this.userLoading = false;
          this.userOptions = JSON.parse(msg.data);
          break
        }
        case 'TERMINAL_SESSION_PAUSE': {
          const data = JSON.parse(msg.data);
          const notifyMsg = `${data.user} ${this.$t('PauseSession')}`
          this.$message(notifyMsg)
          break
        }
        case 'TERMINAL_SESSION_RESUME': {
          const data = JSON.parse(msg.data);
          const notifyMsg  = `${data.user} ${this.$t('ResumeSession')}`
          this.$message(notifyMsg)
          break
        }
        default:
          break
      }
      this.$log.debug("on ws data: ", msg)
    },

    handleChangeTheme(themeName,val) {
      this.updateThemeSetting(themeName, val)
    },
    handleSyncTheme(themeName,val) {
      this.$log.debug(themeName,val);
      if (this.$refs.term) {
        const data = {'terminal_theme_name': themeName}
        this.$refs.term.syncUserPreference(data);
      }

    },
    updateThemeSetting(themeName,val) {
      const themeColors = val || defaultTheme;
      if (this.$refs.term.term) {
        this.$refs.term.term.setOption("theme", themeColors);
        this.themeBackground = themeColors.background;
      }
      this.$log.debug(themeName, val);
    },
    handleShareURlCreated() {
      this.loading = true
      if (this.$refs.term) {
        const req = this.shareLinkRequest;
        this.$refs.term.createShareInfo(
            this.sessionId, req.expiredTime,
            req.users, req.actionPerm);
      }
      this.$log.debug("分享请求数据： ", this.sessionId, this.shareLinkRequest)
    },
    shareDialogClosed() {
      this.$log.debug("share dialog closed")
      this.loading = false;
      this.shareId = null;
      this.shareCode = null;
    },
    getSessionUser(query) {
      if (query !== '' && this.$refs.term) {
        this.userLoading = true;
        this.$refs.term.getUserInfo(query);
      } else {
        this.userOptions = []
      }
    },
    onEvent(event, data) {
      switch (event) {
        case 'reconnect':
          Object.keys(this.onlineUsersMap).filter(key => {
            this.$delete(this.onlineUsersMap, key);
          })
          this.$log.debug("reconnect: ", data);
          break
        case 'open':
          this.$log.debug("open: ", data);
          this.$refs.panel.toggle()
          break
      }
    },
    getMinuteLabel(item) {
      let minuteLabel = this.$t('Minute')
      if (item > 1) {
        minuteLabel = this.$t('Minutes')
      }
      return `${item} ${minuteLabel}`
    },
  },
}
</script>

<style scoped>
.el-menu-item.is-active {
  color: #fff;
}

.settings {
  padding: 24px 20px;
  color: #fff;
}

.el-result {
  padding: 0;
}

.share-btn {
  background-color: #343333;
  color: white;
}

::v-deep .el-form-item__content {
  display: flex;
  flex-direction: column;
}

::v-deep .el-form-item__content > div,
::v-deep .el-form-item__content > span {
  flex: 1;
}
</style>
