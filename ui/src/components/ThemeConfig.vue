<template>
  <div>
    <el-dialog
      :title="this.$t('Theme')"
      :visible.sync="iVisible"
      width="50%"
      class="theme-dialog"
      :close-on-press-escape="false">

      <div class="content">
        <el-form :inline="true" >
          <el-form-item style="width: 73%">
            <el-select v-model="theme" :placeholder="this.$t('SelectTheme')">
              <el-option v-for="item in themes" :key="item" :label="item" :value="item"></el-option>
            </el-select>
          </el-form-item>
          <el-form-item style="width: 20%" v-loading="loading" >
            <el-button class="sync-btn" @click="syncTheme">{{ this.$t('Sync') }}</el-button>
          </el-form-item>
        </el-form>
          <div v-if="Object.keys(colors).length > 0">
            <p class="title">Theme Colors</p>
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
              <el-col :span="4">
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
              <el-col :span="4">
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
import {defaultTheme} from "@/utils/common";
const themes = Object.keys(xtermTheme);
export default {
  name: "ThemeConfig",
  props: {
    visible: Boolean,
    themeName: {
      type: String,
      required: true
    },
  },
  data() {
    return {
      themes: ['Default', ...themes],
      theme: 'Default',
      loading: false,
    };
  },
  computed: {
    colors() {
      if (this.theme && themes.includes(this.theme)) {
        return xtermTheme[this.theme];
      } else {
        return defaultTheme;
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
      const theme = val && val !== 'Default' ? val : '';
      this.$emit("setTheme",theme, xtermTheme[theme]);
    },
    themeName(val) {
      this.theme = val;
    }
  },
  methods: {
    syncTheme() {
      this.loading = true;
      const vm = this;
      this.$emit("syncThemeName",this.theme, xtermTheme[this.theme]);
      // 5s后关闭loading, 避免出现异常
      setTimeout(function () {
        vm.loading = false;
      }, 1000*5);
    }
  }
};
</script>

<style  scoped>
.title {
  font-size: 14px;
  color: #fff;
}
.theme-dialog .el-dialog__header{
  background-color: #303133;
  color: #fff;
}

.theme-colors {
  font-size: 12px;
  color: #fff;
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

.sync-btn {
  background-color: #343333;
  color: white;
}
</style>