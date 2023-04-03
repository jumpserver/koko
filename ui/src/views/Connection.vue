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
        :modal="false"
        center>
      <div v-if="!shareId">
          <el-form v-loading="loading" :model="shareLinkRequest">
            <el-form-item :label="this.$t('Terminal.ExpiredTime')">
              <el-select v-model="shareLinkRequest.expiredTime" :placeholder="this.$t('Terminal.SelectAction')">
                <el-option
                    v-for="item in expiredOptions"
                    :key="item.value"
                    :label="item.label"
                    :value="item.value">
                </el-option>
              </el-select>
            </el-form-item>
            <el-form-item :label="this.$t('Terminal.ActionPerm')">
              <el-select v-model="shareLinkRequest.actionPerm" :placeholder="this.$t('Terminal.ActionPerm')">
                <el-option
                    v-for="item in actionsPermOptions"
                    :key="item.value"
                    :label="item.label"
                    :value="item.value">
                </el-option>
              </el-select>
            </el-form-item>
            <el-form-item :label="this.$t('Terminal.ShareUser')">
                <el-select v-model="shareLinkRequest.users" multiple filterable remote reserve-keyword :placeholder="this.$t('Terminal.GetShareUser')"
                           :remote-method="getSessionUser" :loading="userLoading">
                    <el-option
                      v-for="item in userOptions"
                      :key="item.id"
                      :label="item.name + '(' + item.username + ')'"
                      :value="item.id">
                    </el-option>
                  </el-select>
                  <div style="color: #aaa">{{ this.$t('Terminal.ShareUserHelpText') }}</div>
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
import {BASE_URL, BASE_WS_URL, CopyTextToClipboard, defaultTheme} from "@/utils/common";
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
      shareLinkRequest: {
        expiredTime: 10,
        actionPerm: 'writable',
        users: []
      },
      expiredOptions: [
        {label: "1m", value: 1},
        {label: "5m", value: 5},
        {label: "10m", value: 10},
        {label: "20m", value: 20},
        {label: "60m", value: 60},
      ],
      actionsPermOptions: [
        {label: this.$t('Terminal.Writable'), value: "writable"},
        {label: this.$t('Terminal.ReadOnly'), value: "readonly"},
      ],
      shareId: null,
      loading: false,
      userLoading: false,
      shareCode: null,
      shareInfo: null,
      onlineUsersMap: {},
      userOptions: [],
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
            item.faIcon = item.writable?'fa-solid fa-keyboard':'fa-solid fa-eye'
            item.iconTip = item.writable?this.$t('Terminal.Writable'):this.$t('Terminal.ReadOnly')
            return item
          }).sort((a, b) => new Date(a.created) - new Date(b.created)),
          itemClick: () => {},
          itemActions: [
            {
              faIcon:'fa-solid fa-trash-can',
              tipText: this.$t('Terminal.Remove'),
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
                this.$confirm(this.$t('Terminal.RemoveShareUserConfirm'))
                    .then( () => {
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
          const key = data.terminal_id
          this.$set(this.onlineUsersMap, key, data);
          this.$log.debug(this.onlineUsersMap);
          if (data.primary) {
            this.$log.debug("primary user 不提醒")
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
      const themeColors = val || defaultTheme;
      if (this.$refs.term.term) {
        this.$refs.term.term.setOption("theme", themeColors);
        this.themeBackground = themeColors.background;
      }
      this.$log.debug(val);
    },
    handleShareURlCreated() {
      this.loading = true
      if (this.$refs.term) {
        const req = this.shareLinkRequest;
        this.$refs.term.createShareInfo(
            this.sessionId, req.expiredTime,
            req.users, req.actionPerm);
      }
      this.$log.debug("分享请求数据： ", this.sessionId,this.shareLinkRequest)
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
          this.$log.debug("reconnect: ",data);
          break
      }
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
