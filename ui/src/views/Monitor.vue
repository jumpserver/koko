<template>
  <Terminal v-bind:connectURL="wsURL"></Terminal>
</template>

<script>
import Terminal from "@/components/Terminal";
import {BASE_WS_URL} from "@/utils/common";

export default {
  components: {
    Terminal,
  },
  name: "MonitorTerminal",
  data() {
    return {}
  },
  computed: {
    wsURL() {
      return this.getConnectURL()
    }
  },
  methods: {
    getConnectURL() {
      const params = this.$route.params
      const requireParams = new URLSearchParams();
      requireParams.append('type', "monitor");
      requireParams.append('target_id', params.id);
      return `${BASE_WS_URL}/koko/ws/terminal/?${requireParams.toString()}`
    },
  }
}
</script>

<style scoped>

</style>