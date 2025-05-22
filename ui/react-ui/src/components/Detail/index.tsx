import { Collapse, Card } from 'antd';
import { useState } from 'react';

import type { CollapseProps } from 'antd';

import Theme from './widgets/Theme';
import Overview from './widgets/Overview';
import Appearance from './widgets/Appearance';

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
    children: <Appearance />
  },
  {
    key: '3',
    label: '主题设置',
    children: <Theme />
  }
];

const Detail: React.FC = () => {
  return (
    <Collapse
      expandIconPosition="end"
      items={items}
      bordered={false}
      defaultActiveKey={['1']}
      className="w-full h-full"
    />
  );
};

export default Detail;
