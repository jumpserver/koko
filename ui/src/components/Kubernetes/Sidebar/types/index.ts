import { CSSProperties, Component } from 'vue';

export interface IActionOptions {
    // 唯一标识
    key: string;

    // 操作选项名称
    label: string;

    // 子选项
    children?: Array<IActionOptions>;

    // 是否隐藏
    hidden?: boolean;

    // 点击事件
    click?: () => void;

    // 跳转地址
    href?: string;

    // 是否隐藏
    disable?: boolean;
}

export interface optionsDetail {
    key: string;
    label?: string;
    type?: string;
    render?: () => JSX.Element;
    onClink?: () => void;
}

export interface HeaderRightOptions {
    // icon 名称
    name: string;

    // icon 样式
    iconStyle: CSSProperties;

    // 组件
    component: Component;

    // 下拉菜单选项
    options?: optionsDetail[];

    // 顶层的回调
    onClick?: () => void;
}
