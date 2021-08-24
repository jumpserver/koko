<template>
  <el-dialog
      title="提示"
      :visible.sync="displayVisible"
      width="30%"
      :before-close="handleClose">
    <span>这是一段信息</span>
    <span slot="footer" class="dialog-footer">
    <el-button type="primary" @click="dialogVisible = false">创建共享连接</el-button>
  </span>
  </el-dialog>
</template>

<script>
export default {
  name: "ShareConfig",
  props: {
    visible: Boolean
  },
  data(){
    return {
      displayVisible: this.visible
    }
  }
}
</script>

<style scoped>

</style>