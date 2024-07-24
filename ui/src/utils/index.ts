import { createDiscreteApi } from 'naive-ui';

const { message } = createDiscreteApi(['message']);

/**
 * @description 复制文本功能
 * @param {string} text
 */
export const copyTextToClipboard = async (text: string): Promise<void> => {
    try {
        // Clipboard API
        if (navigator.clipboard && navigator.clipboard.writeText) {
            await navigator.clipboard.writeText(text);
            message.info('Text copied to clipboard');
        } else {
            // Fallback 方式，兼容不支持 Clipboard API 的情况
            let transfer: HTMLTextAreaElement = document.createElement('textarea');

            document.body.appendChild(transfer);
            transfer.value = text;
            transfer.focus();
            transfer.select();

            document.execCommand('copy');
            document.body.removeChild(transfer);

            message.info('Text copied to clipboard (fallback method)');
        }
    } catch (err) {
        message.error(`Failed to copy text: ${err}`);
    }
};
