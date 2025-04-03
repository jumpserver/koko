import { defineConfig } from "eslint/config";
import globals from "globals";
import js from "@eslint/js";
import tseslint from "typescript-eslint";
import pluginVue from "eslint-plugin-vue";


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
  { files: ["**/*.vue"], languageOptions: { parserOptions: { parser: tseslint.parser } }, rules: {
    'vue/multi-word-component-names': 'off',
    'vue/require-default-prop': 'error',
    'vue/attributes-order': 'error',
    'vue/attribute-hyphenation': 'error'
  } },
]);
