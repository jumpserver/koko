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
