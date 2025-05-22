import { useState } from 'react';
import { TextCursor } from 'lucide-react';
import { FontSizeOutlined, FontColorsOutlined } from '@ant-design/icons';
import { Card, Form, InputNumber, Segmented, Switch, Select, Space, Divider, Flex } from 'antd';

const Appearance = () => {
  const [cursorStyle, setCursorStyle] = useState<string | number>('block');
  const handleFontSizeChange = () => {};

  return (
    <Card className="appearance-card" variant="borderless">
      <Space direction="vertical" size="large" style={{ width: '100%' }}>
        <Form layout="vertical" style={{ width: '100%' }}>
          <div style={{ marginBottom: '16px' }}>
            <Divider orientation="left" plain style={{ margin: '8px 0' }}>
              <Space>
                <FontColorsOutlined />
                <span>字体</span>
              </Space>
            </Divider>

            <Flex gap={16} align="start">
              <Form.Item label="字体系列" style={{ flex: 3, marginBottom: '8px' }}>
                <Select
                  placeholder="选择字体"
                  options={[
                    { value: 'monospace', label: 'Monospace' },
                    { value: 'sans-serif', label: 'Sans Serif' },
                    { value: 'serif', label: 'Serif' }
                  ]}
                />
              </Form.Item>

              <Form.Item label="字体大小" style={{ flex: 1, marginBottom: '8px' }}>
                <InputNumber
                  min={8}
                  max={32}
                  keyboard={true}
                  defaultValue={16}
                  onChange={handleFontSizeChange}
                  prefix={<FontSizeOutlined />}
                  style={{ width: '100%' }}
                />
              </Form.Item>
            </Flex>
          </div>

          <div>
            <Divider orientation="left" plain style={{ margin: '8px 0' }}>
              <Space>
                <TextCursor size={14} />
                <span>光标</span>
              </Space>
            </Divider>

            <Flex gap={16} align="start">
              <Form.Item label="光标样式" style={{ flex: 3, marginBottom: '8px' }}>
                <Segmented
                  options={[
                    { value: 'block', label: '块状' },
                    { value: 'underline', label: '下划线' },
                    { value: 'bar', label: '竖线' }
                  ]}
                  value={cursorStyle}
                  onChange={setCursorStyle}
                />
              </Form.Item>

              <Form.Item label="光标闪烁" style={{ flex: 1, marginBottom: '8px' }}>
                <Switch size="default" />
              </Form.Item>
            </Flex>
          </div>
        </Form>
      </Space>
    </Card>
  );
};

export default Appearance;
