import { Descriptions, Divider, Card, Button } from 'antd';

import type { DescriptionsProps } from 'antd';

const items: DescriptionsProps['items'] = [
  {
    key: '1',
    label: 'UserName',
    span: 1,
    children: 'Zhou Maomao'
  },
  {
    key: '2',
    label: 'Telephone',
    span: 1,
    children: '1810000000'
  },
  {
    key: '3',
    label: 'Live',
    span: 1,
    children: 'Hangzhou, Zhejiang'
  }
];

const Overview = () => {
  return (
    <Card variant="borderless" style={{ width: '100%', height: '100%' }}>
      <Descriptions title="Connection Info" items={items} column={1} />
    </Card>
  );
};

export default Overview;
