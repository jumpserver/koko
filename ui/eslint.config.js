import { defineConfig } from "eslint/config";
import globals from "globals";
import js from "@eslint/js";
import tseslint from "typescript-eslint";
import pluginVue from "eslint-plugin-vue";
import spellcheck from "eslint-plugin-spellcheck";

export default defineConfig([
  {
    ignores: [
      'node_modules',
      'dist',
      'public',
    ],
  },
  { files: ["**/*.{js,mjs,cjs,ts,vue}"] },
  { files: ["**/*.{js,mjs,cjs,ts,vue}"], languageOptions: { globals: globals.browser } },
  { files: ["**/*.{js,mjs,cjs,ts,vue}"], plugins: { js }, extends: ["js/recommended"] },
  tseslint.configs.recommended,
  pluginVue.configs["flat/essential"],
  {
    files: ["**/*.{js,mjs,cjs,ts,tsx,vue}"],
    plugins: {
      spellcheck: spellcheck,
    },
    rules: {
      "spellcheck/spell-checker": [
        "warn",
        {
          comments: true,
          strings: true,
          identifiers: false,
          templates: true,
          // 添加项目特定的术语到此处以避免误报
          skipWords: [
            "koko",
            "sftp",
            "cmd",
            "xterm",
            "vue",
            "vuex",
            "pinia",
            "axios",
            "dayjs",
            "tabler",
            "lucide",
            "naiveui",
            "pnpm",
            "websocket",
            "zmodem",
            "keyevent",
            "perm",
            "dirname",
            "basename",
            "filepath",
            "luna",
            // 用户名相关
            "admin",
            // 扩展名和格式
            "json",
            "yaml",
            "yml",
            "tsx",
            "ts",
            "js",
            "mjs",
            "cjs",
            // 中文相关
            "pinyin",
            // 可能的变量名缩写
            "req",
            "res",
            "params",
            "config",
            "util",
            "ctx",
            "el",
            "fn",
            "msg",
            "prev",
            "curr",
            "tmp",
            "args",
            "env",
            "async",
            "init",
            "destructuredArrayIgnorePattern",
          ],
          skipIfMatch: [
            // 忽略URL
            "http://[^ ]*",
            "https://[^ ]*",
            // 忽略导入路径
            "@/[^ ]*",
            // 忽略版本号
            "\\d+\\.\\d+\\.\\d+",
          ],
          minLength: 5,
        }
      ]
    }
  },
  {
    files: ["**/*.vue"],
    languageOptions: { parserOptions: { parser: tseslint.parser } },
    rules: {
      'vue/multi-word-component-names': 'off',
      'vue/require-default-prop': 'error',
      'vue/attributes-order': 'error',
      'vue/attribute-hyphenation': 'error',
      "no-unused-vars": "off",
      '@typescript-eslint/no-unused-vars': [
        "error",
        {
          "args": "all",
          "argsIgnorePattern": "^_",
          "caughtErrors": "all",
          "caughtErrorsIgnorePattern": "^_",
          "destructuredArrayIgnorePattern": "^_",
          "varsIgnorePattern": "^_",
          "ignoreRestSiblings": true
        }
      ],
    }
  },
]);
