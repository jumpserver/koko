import { h } from 'vue';
import { NPopover, TreeOption } from 'naive-ui';
import { customTreeOption } from '@/hooks/interface';

/**
 * @description 用于每个 Tree Item 的 Tooltip
 * @param option
 */
export const showToolTip = (option: TreeOption) => {
  const customOption = option.option as customTreeOption;

  return h(
    NPopover,
    {
      trigger: 'hover',
      placement: 'top'
    },
    {
      trigger: () =>
        h('span', { style: { display: 'inline-block', whiteSpace: 'nowrap' } }, customOption.label),
      default: () => customOption.label
    }
  );
};

/**
 * @description 将 Base64 转化为字节数组
 * @param raw
 */
export const base64ToUint8Array = (base64: string): Uint8Array => {
  // 转为原始的二进制字符串（binaryString）。
  const binaryString = atob(base64);
  const len = binaryString.length;

  const bytes = new Uint8Array(len);
  for (let i = 0; i < len; i++) {
    bytes[i] = binaryString.charCodeAt(i);
  }
  return bytes;
};
