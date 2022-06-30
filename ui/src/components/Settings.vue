<template>
  <div class="setting">
    <h3 class="title">{{ title }}</h3>
    <ul style="padding: 0">
      <li
        v-for="(i, index) in settings"
        class="item"
        :key="index"
      >
        <el-button
          type="text"
          class="item-button"
          :disabled="i.disabled()"
          :class="'icon ' + i.icon"
          @click.stop="i.click && itemClick(i)"
        >
          {{ i.title }}
          {{ i.content && Object.keys(i.content).length > 0 ? Object.keys(i.content).length : null }}
        </el-button>
        <div v-if="i.content" class="content">
          <el-button
              v-for="(item, key) of i.content"
              :key="key"
              class="content-item"
              type="text"
              :disabled="i.disabled()"
              @click="i.itemClick && i.itemClick(item.keys)"
          >
            {{ item.name }}
          </el-button>
        </div>
      </li>
    </ul>
  </div>
</template>

<script>
export default {
  name: 'Settings',
  props: {
    title: {
      type: String,
      required: true
    },
    settings: {
      type: Array,
      default: () => []
    }
  },
  methods: {
    itemClick(item) {
      this.$parent.show = false
      item.click()
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
  padding-left: 10px;
  width: 100%;
  text-align: left;
  color: #000;
}

.item-button.is-disabled {
  color: rgb(0, 0, 0, 0.5);
}

.item-button.is-disabled:hover {
  color: rgb(0, 0, 0, 0.5);
  background: none;
}


.item-button:hover {
  background: rgba(0, 0, 0, .1);
}

.content {
  padding: 4px 6px 4px 25px;
}

.content-item {
  font-size: 13px;
  white-space: nowrap;
  text-overflow: ellipsis;
  overflow: hidden;
  padding: 4px 0;
  color: black;
  margin-left: 0;
  display: block;
  width: 100%;
  text-align: left;
}

.content-item:hover {
  border-radius: 2px;
  background: rgba(0, 0, 0, .1);
}
</style>
