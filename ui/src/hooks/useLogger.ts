import { ref, Ref } from 'vue';
import { useDateNow } from 'vue-composable';

interface ILog {
    level: string;
    message: string;
    timestamp: string;
}

type LogLevel = 'log' | 'info' | 'warn' | 'error' | 'debug';

export const useLogger = () => {
    const logs: Ref<ILog[]> = ref([]);
    const { now } = useDateNow();

    const log = (level: LogLevel, message: string) => {
        const timestamp = new Date(now.value).toISOString();
        logs.value.push({ level, message, timestamp });
        console[level](`${timestamp}: ${message}`);
    };

    const info = (message: string) => log('info', message);
    const warn = (message: string) => log('warn', message);
    const debug = (message: string) => log('debug', message);
    const error = (message: string) => log('error', message);

    return {
        logs,
        debug,
        info,
        warn,
        error
    };
};
