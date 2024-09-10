import { useLogger } from '@/hooks/useLogger.ts';
import { storeToRefs } from 'pinia';
import { useGlobalStore } from '@/store/modules/global';
import { useTerminalStore } from '@/store/modules/terminal.ts';
import { NavigationGuardNext } from 'vue-router';

const { info } = useLogger('Guard');

const onI18nLoaded = () => {
    const globalStore = useGlobalStore();
    const { i18nLoaded } = storeToRefs(globalStore);

    return new Promise(resolve => {
        if (i18nLoaded.value) {
            info('i18n already loaded');
            resolve(true);
        }

        const itv = setInterval(() => {
            if (i18nLoaded.value) {
                clearInterval(itv);
                info('i18n loaded after interval');
                resolve(true);
            }
        }, 100);
    });
};
const getLunaConfig = () => {
    const terminalStore = useTerminalStore();

    const { setTerminalConfig } = terminalStore;

    const localSetting: string | null = localStorage.getItem('LunaSetting');

    console.info(localSetting);

    let fontSize = terminalStore.fontSize;

    if (localSetting !== null) {
        let commandLine = JSON.parse(localSetting)['command_line'];
        if (commandLine) {
            fontSize = commandLine['character_terminal_font_size'];
            setTerminalConfig('quickPaste', commandLine['is_right_click_quickly_paste'] ? '1' : '0');
            setTerminalConfig('backspaceAsCtrlH', commandLine['is_backspace_as_ctrl_h'] ? '1' : '0');
        }
    }

    if (!fontSize || fontSize < 5 || fontSize > 50) {
        setTerminalConfig('fontSize', 13);
    }

    setTerminalConfig('ctrlCAsCtrlZ', '0');
};
const startUp = async (): Promise<boolean> => {
    const globalStore = useGlobalStore();

    const { initialized } = storeToRefs(globalStore);

    if (initialized.value) {
        info('Already initialized');
        return true;
    }

    info('Initializing global store');

    globalStore.init();
    await onI18nLoaded();
    getLunaConfig();

    return true;
};

export const guard = async (next: NavigationGuardNext) => {
    try {
        await startUp();
        next();
    } catch (error) {
        if (error instanceof Error) {
            info(`Start service error: ${error.message}`);
        } else {
            info(`Start service error: ${String(error)}`);
        }
    }
};
