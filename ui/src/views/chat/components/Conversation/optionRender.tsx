import { useI18n } from 'vue-i18n';
import { NSpace, NText } from 'naive-ui';
import { SquareArrowOutUpRight, PencilLine, Trash2 } from 'lucide-vue-next';

import type { SelectOption } from 'naive-ui';
import type { FunctionalComponent } from 'vue';
import type { LucideProps } from 'lucide-vue-next';

interface OptionItem {
  value: string;
  label: string;
  textColor?: string;
  iconColor?: string;
  click: (chatId: string) => void;
  icon: FunctionalComponent<LucideProps>;
}

type EmitsType = {
  (e: 'chat-share', shareId: string): void;
  (e: 'chat-rename', shareId: string): void;
  (e: 'chat-delete', shareId: string): void;
};

export const OptionRender = (emits: EmitsType): SelectOption[] => {
  const { t } = useI18n();

  const optionItems: OptionItem[] = [
    {
      value: 'share',
      icon: SquareArrowOutUpRight,
      label: t('Share'),
      iconColor: 'white',
      click: (chatId: string) => {
        emits('chat-share', chatId);
      }
    },
    {
      value: 'rename',
      icon: PencilLine,
      label: t('Rename'),
      iconColor: 'white',
      click: (chatId: string) => {
        emits('chat-rename', chatId);
      }
    },
    {
      value: 'delete',
      icon: Trash2,
      label: t('Delete'),
      iconColor: '#fb2c36',
      textColor: '!text-red-500',
      click: (chatId: string) => {
        emits('chat-delete', chatId);
      }
    }
  ];

  const commonClass = 'px-4 py-2 w-30 hover:bg-[#ffffff1A] cursor-pointer transition-all duration-300';

  return optionItems.map(item => ({
    value: item.value,
    render: () => (
      <NSpace align="center" class={commonClass} onClick={() => item.click(item.value)}>
        {item.icon && <item.icon color={item.iconColor || 'white'} size={16} />}

        <NText depth={1} class={`text-sm ${item.textColor || ''}`}>
          {item.label}
        </NText>
      </NSpace>
    )
  }));
};
