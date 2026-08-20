import { computed } from 'vue';
import { useI18n } from 'vue-i18n';
import { useMessage } from 'naive-ui';

import type { ClipboardAccess, ClipboardDirection, ClipboardValidationResult } from '@/types/modules/clipboard.type';

import { useTerminalStore } from '@/store/modules/terminal';
import { useClipboardStore } from '@/store/modules/clipboard';
import { validateClipboardText as validateText } from '@/utils/clipboardAcl';

export const useClipboardAcl = () => {
  const { t } = useI18n();
  const message = useMessage();
  const clipboardStore = useClipboardStore();
  const terminalStore = useTerminalStore();

  const isK8sEnvironment = computed(() => window.location.pathname.includes('/k8s'));
  const access = computed(() => {
    const sessionId = isK8sEnvironment.value ? terminalStore.currentTab : undefined;
    return clipboardStore.getAccess(sessionId);
  });

  const notifyValidationFailure = (direction: ClipboardDirection, result: ClipboardValidationResult) => {
    if (result.allowed)
      return;

    if (result.reason === 'text_limit') {
      message.warning(
        t('ClipboardTextLimitExceeded', {
          action: t(direction === 'copy' ? 'Copy' : 'Paste'),
          limit: result.limit,
        }),
      );
      return;
    }

    message.warning(t(direction === 'copy' ? 'ClipboardCopyDenied' : 'ClipboardPasteDenied'));
  };

  const validateClipboardText = (
    direction: ClipboardDirection,
    text: string,
    accessOverride?: ClipboardAccess,
  ): boolean => {
    const result = validateText(accessOverride ?? access.value, direction, text);
    notifyValidationFailure(direction, result);
    return result.allowed;
  };

  return {
    access,
    validateClipboardText,
    notifyValidationFailure,
  };
};
