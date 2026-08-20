import { defineStore } from 'pinia';

import type { ClipboardAccess, ClipboardPermission, ClipboardPolicy } from '@/types/modules/clipboard.type';

import { createUnrestrictedClipboardAccess, resolveClipboardAccess } from '@/utils/clipboardAcl';

interface ClipboardState {
  defaultAccess: ClipboardAccess;
  sessionAccess: Record<string, ClipboardAccess>;
}

export const useClipboardStore = defineStore('clipboard', {
  state: (): ClipboardState => ({
    defaultAccess: createUnrestrictedClipboardAccess(),
    sessionAccess: {},
  }),
  getters: {
    getAccess:
      state =>
        (sessionId?: string): ClipboardAccess => {
          if (sessionId && state.sessionAccess[sessionId]) {
            return state.sessionAccess[sessionId];
          }

          return state.defaultAccess;
        },
  },
  actions: {
    initialize(permission?: ClipboardPermission | null, policy?: ClipboardPolicy | null) {
      this.defaultAccess = resolveClipboardAccess(permission, policy);
      this.sessionAccess = {};
    },
    setDefaultAccess(permission?: ClipboardPermission | null, policy?: ClipboardPolicy | null) {
      this.defaultAccess = resolveClipboardAccess(permission, policy);
    },
    setSessionAccess(sessionId: string, permission?: ClipboardPermission | null, policy?: ClipboardPolicy | null) {
      this.sessionAccess[sessionId] = resolveClipboardAccess(permission, policy);
    },
    removeSessionAccess(sessionId: string) {
      delete this.sessionAccess[sessionId];
    },
  },
});
