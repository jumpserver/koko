import xtermTheme from 'xterm-theme';

import { storeToRefs } from 'pinia';
import { ref, computed, nextTick } from 'vue';
import { defaultTheme } from '@/config';
import { writeText, readText } from 'clipboard-polyfill';
import { darkTheme, createDiscreteApi } from 'naive-ui';

import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import { SearchAddon } from '@xterm/addon-search';

import { useTerminalSettingsStore } from '@/store/modules/terminalSettings';

import type { ConfigProviderProps } from 'naive-ui';

/**
 * @description 终端控制器
 * @param config
 */
export const useTerminalInstance = () => {
  let fitAddon = new FitAddon();
  let searchAddon = new SearchAddon();

  let terminalSelectionText = ref<string>('');
  let terminalInstance = ref<Terminal>();

  const configProviderPropsRef = computed<ConfigProviderProps>(() => ({
    theme: darkTheme
  }));

  const terminalSettingsStore = useTerminalSettingsStore();

  const { message } = createDiscreteApi(['message'], {
    configProviderProps: configProviderPropsRef
  });

  /**
   * @description 加载终端适配器以及初始化载终端事件
   * @param terminal
   */
  const initializeTerminalEvent = (terminal: Terminal) => {
    terminal.loadAddon(fitAddon);
    terminal.loadAddon(searchAddon);

    terminal.onResize(({ cols, rows }) => {
      console.log('resize', cols, rows);
      fitAddon.fit();
    });
    terminal.onSelectionChange(async () => {
      terminalSelectionText.value = terminal.getSelection().trim();

      if (!terminalSelectionText.value) {
        return;
      }

      // TODO: 国际化 => Unable to copy content to the clipboard. Please check your browser settings or permissions.
      await writeText(terminalSelectionText.value);
    });
    terminal.onData((data) => {
      console.log('data', data);
    })
  };

  const initializeElementEvent = (el: HTMLElement) => {
    el.addEventListener(
      'mouseenter',
      () => {
        fitAddon.fit();
        terminalInstance.value?.focus();
      }
    );

    el.addEventListener(
      'contextmenu',
      async (e: MouseEvent) => {
        if (e.ctrlKey || terminalSettingsStore.quickPaste !== '1') return;

        let text: string = '';

        try {
          text = await readText();
        } catch(e) {
          terminalSelectionText.value ? text = terminalSelectionText.value : '';
        }

        e.preventDefault();

        // Socket Send 
      },
      false
    )
  };
  /**
   * @description 搜索关键字
   * @param keyword 
   * @param type 
   */
  const searchKeyWord = (keyword: string, type: string) => {}

  /**
   * @description  创建终端实例
   */
  const createTerminalInstance = (el: HTMLElement): Terminal => {
    // terminal 设置
    const { fontSize, lineHeight, fontFamily } = storeToRefs(terminalSettingsStore);

    // 创建终端实例
    terminalInstance.value = new Terminal({
      allowProposedApi: true,
      rightClickSelectsWord: true,
      scrollback: 5000,
      // theme: defaultTheme,
      theme: xtermTheme['ENCOM'],
      fontSize: fontSize?.value,
      lineHeight: lineHeight?.value,
      fontFamily: fontFamily?.value
    });

    // 添加适配器以及初始化终端事件
    initializeElementEvent(el);
    initializeTerminalEvent(terminalInstance.value);

    // 终端的实际 open 交由组件控制
    return terminalInstance.value;
  };
  /**
   * @description 设置终端主题
   */
  const setTerminalTheme = (themeName: string) => {
    const theme = xtermTheme[themeName] || defaultTheme;

    nextTick(() => {
      terminalInstance.value!.options.theme = theme;
    })
  };

  return {
    setTerminalTheme,
    createTerminalInstance
  };
};
