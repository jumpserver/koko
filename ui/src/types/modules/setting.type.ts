import type { FunctionalComponent } from 'vue';

export interface SettingConfig {
  theme?: string;
  drawerTitle: string;

  items: Array<{
    type: 'select' | 'keyboard' | 'create' | 'list';
    label: string;
    labelIcon: FunctionalComponent;
    labelStyle: {
      fontSize: string;
    };
    showMore?: boolean;
    disabled?: boolean;
    value?: string;
    options?: any;
  }>;
}
