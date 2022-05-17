<template>
  <div class="setting">
    <h3 class="title">{{ this.$t('Terminal.Settings') }}</h3>
    <ul style="padding: 0">
      <li
        v-for="(i, index) in settings"
        class="item"
        :key="index"
      >
        <el-button
          type="text"
          class="item-button"
          :disabled="!i.disabled()"
          :class="'icon ' + i.icon"
          @click.stop="i.click && i.click()"
        >
          {{ i.title }}
          {{ i.content && Object.keys(i.content).length > 0 ? Object.keys(i.content).length : null }}
        </el-button>
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
    settings: {
      type: Array,
      default: () => []
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
  font-size: 18px;
  color: #000;
}

.item {
  color: rgba(0, 0, 0, 0.65);
  font-size: 14px;
  list-style-type: none;
  cursor: pointer;
  border-radius: 2px;
  line-height: 14px;
}

.item-button {
  width: 100%;
  text-align: left;
}

.item-button:hover {
  background: rgba(0, 0, 0, .3);
}

.content {
  padding: 4px 6px 4px 20px;
}

.content-item {
  white-space: nowrap;
  text-overflow: ellipsis;
  overflow: hidden;
  padding: 4px 0;
  color: black;
}

.content-item:hover {
  color: white;
  border-radius: 2px;
  background: rgba(0, 0, 0, .3);
}
</style>
