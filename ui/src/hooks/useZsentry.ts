import ZmodemBrowser, { SentryConfig } from 'nora-zmodemjs/src/zmodem_browser';

export const useZsentry = () => {
  const createZsentry = (zsentryConfig: SentryConfig) => {
    return new ZmodemBrowser.Sentry(zsentryConfig);
  };

  return {
    createZsentry
  };
};
