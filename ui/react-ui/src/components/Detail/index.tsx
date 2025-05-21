import { Collapse } from 'antd';

import type { CollapseProps } from 'antd';

import Overview from './widgets/Overview';

const text = (
  <p style={{ paddingInlineStart: 24 }}>
    A dog is a type of domesticated animal. Known for its loyalty and faithfulness, it can be found as a welcome guest
    in many households across the world.
  </p>
);

const items: CollapseProps['items'] = [
  {
    key: '1',
    label: '概览',
    children: <Overview />
  },
  {
    key: '2',
    label: '外观设置',
    children: text
  },
  {
    key: '3',
    label: '主题设置',
    children: text
  }
];

const Detail: React.FC = () => {
  return <Collapse items={items} bordered={false} defaultActiveKey={['1']} className="w-full h-full" />;
};

export default Detail;
