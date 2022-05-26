<template>
  <div>
    <el-dialog
      :title="this.$t('Terminal.Theme')"
      :visible.sync="iVisible"
      width="50%"
      :close-on-press-escape="false">

      <div class="content">
        <el-select v-model="theme" :placeholder="this.$t('Terminal.SelectTheme')" style="width: 100%">
            <el-option v-for="item in themes" :key="item" :label="item" :value="item"></el-option>
          </el-select>
          <div v-if="theme">
            <p class="title">{{ this.$t('Terminal.ThemeColors') }}</p>
            <el-row type="flex" class="theme-colors">
              <el-col :span="8">
                <div class="show-color" :style="{backgroundColor: colors.background}"></div>
                <div>Background</div>
              </el-col>
              <el-col :span="8">
                <div class="show-color" :style="{backgroundColor: colors.foreground}"></div>
                <div>Foreground</div>
              </el-col>
              <el-col :span="8">
                <div class="show-color" :style="{backgroundColor: colors.cursor}"></div>
                <div>Cursor</div>
              </el-col>
            </el-row>
            <p class="title">ANSI Colors</p>
            <el-row type="flex" class="theme-colors">
              <el-col :span="3">
                <div class="show-color" :style="{backgroundColor: colors.black}"></div>
                <div>Black</div>
              </el-col>
              <el-col :span="3">
                <div class="show-color" :style="{backgroundColor: colors.red}"></div>
                <div>Red</div>
              </el-col>
              <el-col :span="3">
                <div class="show-color" :style="{backgroundColor: colors.green}"></div>
                <div>Green</div>
              </el-col>
              <el-col :span="3">
                <div class="show-color" :style="{backgroundColor: colors.yellow}"></div>
                <div>Yellow</div>
              </el-col>
              <el-col :span="3">
                <div class="show-color" :style="{backgroundColor: colors.blue}"></div>
                <div>Blue</div>
              </el-col>
              <el-col :span="3">
                <div class="show-color" :style="{backgroundColor: colors.magenta}"></div>
                <div>Magenta</div>
              </el-col>
              <el-col :span="3">
                <div class="show-color" :style="{backgroundColor: colors.cyan}"></div>
                <div>Cyan</div>
              </el-col>
              <el-col :span="3">
                <div class="show-color" :style="{backgroundColor: colors.white}"></div>
                <div>White</div>
              </el-col>
            </el-row>
            <el-row type="flex" class="theme-colors">
              <el-col :span="3">
                <div class="show-color" :style="{backgroundColor: colors.brightBlack}"></div>
                <div>BrightBlack</div>
              </el-col>
              <el-col :span="3">
                <div class="show-color" :style="{backgroundColor: colors.brightRed}"></div>
                <div>BrightRed</div>
              </el-col>
              <el-col :span="3">
                <div class="show-color" :style="{backgroundColor: colors.brightGreen}"></div>
                <div>BrightGreen</div>
              </el-col>
              <el-col :span="3">
                <div class="show-color" :style="{backgroundColor: colors.brightYellow}"></div>
                <div>BrightYellow</div>
              </el-col>
              <el-col :span="3">
                <div class="show-color" :style="{backgroundColor: colors.brightBlue}"></div>
                <div>BrightBlue</div>
              </el-col>
              <el-col :span="3">
                <div class="show-color" :style="{backgroundColor: colors.brightMagenta}"></div>
                <div>BrightMagenta</div>
              </el-col>
              <el-col :span="3">
                <div class="show-color" :style="{backgroundColor: colors.brightCyan}"></div>
                <div>BrightCyan</div>
              </el-col>
              <el-col :span="3">
                <div class="show-color" :style="{backgroundColor: colors.brightWhite}"></div>
                <div>BrightWhite</div>
              </el-col>
            </el-row>
          </div>
      </div>

    </el-dialog>
  </div>
</template>

<script>
import xtermTheme from "xterm-theme";
const themes = Object.keys(xtermTheme);
export default {
  name: "ThemeConfig",
  props: {
    visible: Boolean
  },
  data() {
    return {
      themes: themes,
      theme: window.localStorage.getItem("themeName") || 'Afterglow',
    };
  },
  computed: {
    colors() {
      if (this.theme) {
        return xtermTheme[this.theme];
      } else {
        return null;
      }
    },
    iVisible: {
      set(val) {
        this.$emit('update:visible', val)
      },
      get() {
        return this.visible
      }
    }
  },
  watch: {
    theme(val) {
      window.localStorage.setItem("themeName", val);
      this.$emit("setTheme", xtermTheme[val]);
    }
  },
  mounted() {
    this.$emit("setTheme", xtermTheme[this.theme]);
  }
};
</script>

<style  scoped>
.title {
  font-size: 14px;
}
.theme-colors {
  font-size: 12px;
}
.theme-colors  .show-color {
  width: 100%;
  height: 24px;
  margin-bottom: 10px;
}
.theme-colors  .el-col {
  text-align: center;
}

.theme-colors  .bgimg-btn {
  width: 600px;
  height: 300px;
}

</style>