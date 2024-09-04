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
            placement: 'top-start',
            delay: 1000
        },
        {
            trigger: () =>
                h('span', { style: { display: 'inline-block', whiteSpace: 'nowrap' } }, customOption.label),
            default: () => customOption.label
        }
    );
};
