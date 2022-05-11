<template>
  <div class="setting">
    <h3 class="title">{{ this.$t('Terminal.Settings') }}</h3>
    <ul style="padding: 0">
      <li
        v-for="(i, index) in displaySettings"
        class="item"
        :key="index"
        @click.stop="i.click && i.click()"
      >
        <i :class="'icon ' + i.icon" />
        <span class="text">{{ i.title }} {{ i.content && onlineUserNumbers > 1 ? onlineUserNumbers : null }}</span>
        <div v-if="i.content" class="content">
          <div
            v-for="(item, key) of i.content"
            :key="key"
            class="content-item"
          >
            {{ item.user }}
          </div>
        </div>
      </li>
    </ul>
  </div>
</template>

<script>
export default {
  name: 'Settings',
  props: {
    onlineUsersMap: {
      type: Object,
      default: () => {}
    },
    enableShare: {
      type: Boolean,
      default: () => false
    },
    shareDialogVisible: {
      type: Boolean,
      default: () => false
    },
    dialogVisible: {
      type: Boolean,
      default: () => false
    }
  },
  data() {
    const vm = this
    return {
      settings: [
        {
          title: this.$t('Terminal.ThemeConfig'),
          icon: 'el-icon-orange',
          hidden: () => true,
          click: () => (this.$emit('update:dialogVisible', !this.dialogVisible)),
        },
        {
          title: this.$t('Terminal.Share'),
          icon: 'el-icon-share',
          hidden: () => vm.enableShare,
          click: () => (this.$emit('update:shareDialogVisible', !this.shareDialogVisible)),
        },
        {
          title: this.$t('Terminal.User'),
          icon: 'el-icon-s-custom',
          hidden: () => vm.hiddenOnlineUser,
          content: vm.onlineUsersMap,
        }
      ]
    }
  },
  computed: {
    onlineUserNumbers() {
      return Object.keys(this.onlineUsersMap).length
    },
    hiddenOnlineUser() {
      return this.onlineUserNumbers > 1
    },
    displaySettings() {
      return this.settings.filter(i => i.hidden && i.hidden())
    }
  }
}
</script>

<style scoped>
.setting {
  padding: 24px 24px;
}

.title {
  text-align: left;
  padding-left: 12px;
}

.item {
  color: rgba(0,0,0,0.65);
  font-size: 14px;
  padding: 12px;
  list-style-type: none;
  cursor: pointer;
  border-radius: 2px;
  line-height: 14px;
}

.item:hover {
  color: white;
  background: rgba(0, 0, 0, .3);;
}

.item .text {
  padding-left: 5px;
}

.content {
  padding: 4px 6px;
}

.content-item {
  white-space: nowrap;
  text-overflow: ellipsis;
  overflow: hidden;
  padding: 2px 0;
  color: black;
}
</style>
