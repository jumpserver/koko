import { useState, useRef } from 'react';
import { TextCursor, UserRoundPen } from 'lucide-react';
import { FontSizeOutlined, FontColorsOutlined } from '@ant-design/icons';
import { Card, Form, InputNumber, Segmented, Switch, Select, Space, Divider, Flex } from 'antd';

import useDetail from '@/store/useDetail';

import type { InputNumberProps } from 'antd';

const Appearance = () => {
  const { setTerminalConfig, terminalConfig } = useDetail();

  const handleFontSizeChange: InputNumberProps['onChange'] = (value: number | string | null) => {
    setTerminalConfig({
      fontSize: value
    });
  };

  const handleFontFamilyChange = (value: string) => {
    setTerminalConfig({
      fontFamily: value
    });
  };

  const handleCursorStyleChange = (value: 'block' | 'underline' | 'bar' | 'outline') => {
    setTerminalConfig({
      cursorStyle: value
    });
  };

  const handleCursorBlinkChange = (checked: boolean) => {
    setTerminalConfig({
      cursorBlink: checked
    });
  };

  const fontFamilyOptions = useRef([
    {
      label: <span>Sans Serif</span>,
      title: 'sans-serif',
      options: [{ label: <span>Open Sans</span>, value: 'Open Sans' }]
    },
    {
      label: <span>Monospace</span>,
      title: 'monospace',
      options: [
        { label: <span>Menlo</span>, value: 'Menlo' },
        { label: <span>JetBrains Mono</span>, value: 'JetBrains Mono' },
        { label: <span>Consolas</span>, value: 'Consolas' },
        { label: <span>Fira Code</span>, value: 'Fira Code' },
        { label: <span>Source Code Pro</span>, value: 'Source Code Pro' },
        { label: <span>Monaco</span>, value: 'Monaco' }
      ]
    }
  ]);

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
                  defaultValue={terminalConfig.fontFamily}
                  options={fontFamilyOptions.current}
                  onChange={handleFontFamilyChange}
                />
              </Form.Item>

              <Form.Item label="字体大小" style={{ flex: 1, marginBottom: '8px' }}>
                <InputNumber
                  min={8}
                  max={32}
                  keyboard={true}
                  defaultValue={terminalConfig.fontSize!}
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
                  value={terminalConfig.cursorStyle}
                  onChange={handleCursorStyleChange}
                />
              </Form.Item>

              <Form.Item label="光标闪烁" style={{ flex: 1, marginBottom: '8px' }}>
                <Switch size="default" onChange={handleCursorBlinkChange} checked={terminalConfig.cursorBlink} />
              </Form.Item>
            </Flex>
          </div>

          <div>
            <Divider orientation="left" plain style={{ margin: '8px 0' }}>
              <Space>
                <UserRoundPen size={14} />
                <span>操作偏好</span>
              </Space>
            </Divider>
          </div>
        </Form>
      </Space>
    </Card>
  );
};

export default Appearance;
