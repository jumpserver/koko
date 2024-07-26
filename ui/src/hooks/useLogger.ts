import log, { LogLevelDesc, Logger } from 'loglevel';
import { Ref, ref } from 'vue';

interface UseLogger {
  logLevel: Ref<LogLevelDesc>;
  setLogLevel: (level: LogLevelDesc) => void;
  debug: (...args: any[]) => void;
  info: (...args: any[]) => void;
  warn: (...args: any[]) => void;
  error: (...args: any[]) => void;
}

export const useLogger = (moduleName: string): UseLogger => {
  const logLevel = ref<LogLevelDesc>(log.levels.DEBUG);

  const logger: Logger = log.getLogger(moduleName);
  logger.setLevel(logLevel.value);

  const setLogLevel = (level: LogLevelDesc) => {
    logLevel.value = level;
    logger.setLevel(level);
  };

  const debug = (...args: any[]) => logger.debug(`[${moduleName}] ->`, ...args);
  const info = (...args: any[]) => logger.info(`[${moduleName}] ->`, ...args);
  const warn = (...args: any[]) => logger.warn(`[${moduleName}] ->`, ...args);
  const error = (...args: any[]) => logger.error(`[${moduleName}] ->`, ...args);

  return {
    logLevel,
    setLogLevel,
    debug,
    info,
    warn,
    error
  };
};
