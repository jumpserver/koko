import { Collapse, type CollapseProps } from 'antd';

import Overview from './widgets/Overview';
import Appearance from './widgets/Appearance';

const items: CollapseProps['items'] = [
  {
    key: 'overview',
    label: '概览',
    children: <Overview />
  },
  {
    key: 'appearance',
    label: '外观设置',
    children: <Appearance />
  }
];

const Detail: React.FC = () => {
  return (
    <Collapse
      expandIconPosition="end"
      items={items}
      bordered={false}
      defaultActiveKey={['overview']}
      className="w-full h-full"
    />
  );
};

export default Detail;
